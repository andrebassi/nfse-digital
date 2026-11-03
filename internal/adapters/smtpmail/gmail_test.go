package smtpmail

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"nfse/internal/ports"
)

// fakeServer é um SMTP mínimo em 127.0.0.1: confere a senha no AUTH PLAIN,
// recusa destinatário que começa com "recusado" e guarda o que chegou no DATA.
// mode muda o comportamento: "starttls" anuncia STARTTLS e falha nele;
// "silent" aceita a conexão e nunca responde; "data" recusa o fim do DATA.
type fakeServer struct {
	addr     string
	password string
	mode     string
	mu       sync.Mutex
	data     string
	rcpts    []string
}

// startFake sobe o servidor e o derruba no fim do teste.
func startFake(t *testing.T, password, mode string) *fakeServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	server := &fakeServer{addr: listener.Addr().String(), password: password, mode: mode}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.serve(conn)
		}
	}()
	return server
}

// serve conversa com um cliente até o QUIT.
func (s *fakeServer) serve(conn net.Conn) {
	defer conn.Close()
	if s.mode == "silent" {
		_, _ = io.Copy(io.Discard, conn)
		return
	}
	reader := bufio.NewReader(conn)
	reply := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	reply("220 fake ESMTP")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(command)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			if s.mode == "starttls" {
				reply("250-fake")
				reply("250-STARTTLS")
			} else {
				reply("250-fake")
			}
			reply("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "STARTTLS"):
			reply("454 TLS indisponível")
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			decoded, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(command[len("AUTH PLAIN"):]))
			parts := strings.Split(string(decoded), "\x00")
			if len(parts) == 3 && parts[2] == s.password {
				reply("235 ok")
			} else if len(parts) == 3 && parts[2] == "erro-500" {
				reply("454 temporário")
			} else {
				reply("535 5.7.8 Username and Password not accepted")
			}
		case strings.HasPrefix(upper, "MAIL FROM"):
			reply("250 ok")
		case strings.HasPrefix(upper, "RCPT TO"):
			if strings.Contains(command, "recusado") {
				reply("550 não existe")
				continue
			}
			s.mu.Lock()
			s.rcpts = append(s.rcpts, command)
			s.mu.Unlock()
			reply("250 ok")
		case upper == "DATA":
			reply("354 manda")
			var data strings.Builder
			for {
				part, err := reader.ReadString('\n')
				if err != nil || part == ".\r\n" {
					break
				}
				data.WriteString(part)
			}
			s.mu.Lock()
			s.data = data.String()
			s.mu.Unlock()
			if s.mode == "data" {
				reply("552 grande demais")
			} else {
				reply("250 aceito")
			}
		case upper == "QUIT":
			reply("221 tchau")
			return
		default:
			reply("250 ok")
		}
	}
}

// sample é o e-mail usado nos testes, com acento no assunto e no nome do anexo.
func sample() ports.Mail {
	return ports.Mail{
		From: "dono@gmail.example", FromName: "Financeiro · Bassi", To: []string{"a@cliente.example", "b@cliente.example"},
		CC: []string{"c@cliente.example"}, Subject: "NFS-e nº 42 - EMPRESA", Body: "Olá,\nSegue a nota.\n",
		Attachments: []ports.MailAttachment{
			{Name: "danfse-42.pdf", ContentType: "application/pdf", Data: []byte(strings.Repeat("%PDF", 40))},
			{Name: "nfse-42.xml", ContentType: "application/xml", Data: []byte("<NFSe/>")},
		},
	}
}

// TestSendDelivers: entrega aos três destinatários (Para e Cópia), e a
// mensagem que chega é MIME válido com assunto, texto e os dois anexos.
func TestSendDelivers(t *testing.T) {
	server := startFake(t, "abcdefghijklmnop", "")
	gmail := Gmail{Addr: server.addr, Now: func() time.Time { return time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC) }}
	if err := gmail.Send(context.Background(), "abcdefghijklmnop", sample()); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.rcpts) != 3 {
		t.Fatalf("destinatários: %v", server.rcpts)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(server.data))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if subject != "NFS-e nº 42 - EMPRESA" || parsed.Header.Get("Cc") != "c@cliente.example" || parsed.Header.Get("Date") != "Mon, 02 Nov 2026 19:00:00 +0000" {
		t.Fatalf("cabeçalho: %q %v", subject, parsed.Header)
	}
	_, params, _ := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	reader := multipart.NewReader(parsed.Body, params["boundary"])
	var names []string
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		content, _ := io.ReadAll(part)
		if name := part.FileName(); name != "" {
			names = append(names, name)
			// O leitor multipart não decodifica base64 (só quoted-printable).
			decoded, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(content), "\r\n", ""))
			if name == "nfse-42.xml" && string(decoded) != "<NFSe/>" {
				t.Errorf("XML: %q", decoded)
			}
		} else if !strings.Contains(string(content), "Segue a nota.") {
			t.Errorf("texto: %q", content)
		}
	}
	if strings.Join(names, ",") != "danfse-42.pdf,nfse-42.xml" {
		t.Fatalf("anexos: %v", names)
	}
}

// TestVerifyAndRefusals: senha certa passa no Verify; 535 vira
// ErrLoginRefused; outra recusa no AUTH, STARTTLS quebrado, destinatário
// recusado, DATA recusado, servidor mudo (prazo) e porta fechada são erro.
func TestVerifyAndRefusals(t *testing.T) {
	ctx := context.Background()
	server := startFake(t, "certa", "")
	gmail := Gmail{Addr: server.addr}
	if err := gmail.Verify(ctx, "dono@gmail.example", "certa"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := gmail.Verify(ctx, "dono@gmail.example", "errada"); !errors.Is(err, ErrLoginRefused) {
		t.Fatalf("senha errada: %v", err)
	}
	if err := gmail.Verify(ctx, "dono@gmail.example", "erro-500"); err == nil || errors.Is(err, ErrLoginRefused) {
		t.Fatalf("recusa temporária: %v", err)
	}
	refused := sample()
	refused.To = []string{"recusado@cliente.example"}
	if err := gmail.Send(ctx, "certa", refused); err == nil || !strings.Contains(err.Error(), "recusado@cliente.example") {
		t.Fatalf("destinatário recusado: %v", err)
	}
	if err := gmail.Send(ctx, "errada", sample()); !errors.Is(err, ErrLoginRefused) {
		t.Fatalf("send com senha errada: %v", err)
	}
	dataRefused := startFake(t, "certa", "data")
	if err := (Gmail{Addr: dataRefused.addr}).Send(ctx, "certa", sample()); err == nil || !strings.Contains(err.Error(), "não aceitou") {
		t.Fatalf("DATA recusado: %v", err)
	}
	tlsBroken := startFake(t, "certa", "starttls")
	if err := (Gmail{Addr: tlsBroken.addr}).Verify(ctx, "dono@gmail.example", "certa"); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("STARTTLS: %v", err)
	}
	silent := startFake(t, "certa", "silent")
	started := time.Now()
	if err := (Gmail{Addr: silent.addr, Timeout: 200 * time.Millisecond}).Verify(ctx, "dono@gmail.example", "certa"); err == nil || time.Since(started) > 5*time.Second {
		t.Fatalf("servidor mudo: %v em %v", err, time.Since(started))
	}
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if err := (Gmail{Addr: silent.addr}).Verify(short, "dono@gmail.example", "certa"); err == nil {
		t.Fatal("prazo do context ignorado")
	}
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := closed.Addr().String()
	closed.Close()
	if err := (Gmail{Addr: addr}).Verify(ctx, "dono@gmail.example", "certa"); err == nil || !strings.Contains(err.Error(), "não conectou") {
		t.Fatalf("porta fechada: %v", err)
	}
	if err := (Gmail{Addr: "sem-porta"}).Verify(ctx, "a@b.example", "x"); err == nil {
		t.Fatal("endereço sem porta devia falhar")
	}
	if err := gmail.Send(ctx, "certa", ports.Mail{From: "a@b.example"}); err == nil {
		t.Fatal("sem destinatário devia falhar antes de conectar")
	}
}

// TestBuildRefusesInjection: quebra de linha no assunto ou no nome é recusada
// (injeção de cabeçalho); endereço inválido também; texto longo e anexo
// grande saem em linhas dentro do limite do SMTP.
func TestBuildRefusesInjection(t *testing.T) {
	now := time.Now()
	for name, change := range map[string]func(*ports.Mail){
		"assunto":      func(m *ports.Mail) { m.Subject = "oi\r\nBcc: atacante@example.com" },
		"nome":         func(m *ports.Mail) { m.FromName = "x\nBcc: y@example.com" },
		"destinatário": func(m *ports.Mail) { m.To = []string{"não é e-mail"} },
	} {
		message := sample()
		change(&message)
		if _, err := Build(message, now); err == nil {
			t.Errorf("%s: aceitou", name)
		}
	}
	message := sample()
	message.CC = nil
	message.Attachments[0].Data = make([]byte, 4096)
	data, err := Build(message, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Cc:") {
		t.Error("Cc vazio saiu no cabeçalho")
	}
	for line := range strings.SplitSeq(string(data), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("linha de %d bytes estoura o limite do SMTP", len(line))
		}
	}
	if id := messageID("sem-arroba"); !strings.HasSuffix(id, "@nfse-digital>") {
		t.Errorf("Message-ID sem domínio: %s", id)
	}
}
