// Package smtpmail envia o e-mail da nota pelo SMTP do Gmail com senha de app
// (verificação em duas etapas ligada na conta Google). Sem OAuth: é o mesmo
// caminho mais comum para Gmail, e a senha de app pode ser revogada pelo dono a
// qualquer hora em myaccount.google.com/apppasswords.
package smtpmail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"nfse/internal/ports"
)

// DefaultAddr é o SMTP do Gmail com STARTTLS. A 465 (TLS direto) também
// existiria; a 587 é a que o Google documenta para cliente de e-mail.
const DefaultAddr = "smtp.gmail.com:587"

// defaultTimeout limita a conversa inteira com o Gmail. Sem ele, um servidor
// que aceita a conexão e não responde prenderia a emissão (o envio automático
// roda logo depois da autorização).
const defaultTimeout = 30 * time.Second

// ErrLoginRefused é o Gmail recusando usuário e senha (SMTP 535): quase
// sempre senha normal no lugar da senha de app, ou senha de app revogada.
var ErrLoginRefused = errors.New("o Gmail recusou o login: use uma senha de app de 16 letras (myaccount.google.com/apppasswords), não a senha normal da conta")

// Gmail fala com o servidor SMTP. Addr vazio usa o do Gmail; o painel local
// pode apontar para um servidor de teste (NFSE_SMTP_ADDR).
type Gmail struct {
	Addr    string
	Timeout time.Duration
	// Now carimba a data do e-mail; nil usa o relógio do sistema.
	Now func() time.Time
}

// compile-time: garante que o adapter cumpre o port.
var _ ports.Mailer = Gmail{}

// Verify faz só o login, sem mandar nada: é a conferência ao salvar.
func (g Gmail) Verify(ctx context.Context, address, password string) error {
	client, err := g.open(ctx, address, password)
	if err != nil {
		return err
	}
	return client.Quit()
}

// Send entrega o e-mail a todos os destinatários (Para e Cópia).
func (g Gmail) Send(ctx context.Context, password string, message ports.Mail) error {
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	data, err := Build(message, now())
	if err != nil {
		return err
	}
	client, err := g.open(ctx, message.From, password)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Mail(message.From); err != nil {
		return fmt.Errorf("remetente recusado: %w", err)
	}
	for _, recipient := range append(append([]string{}, message.To...), message.CC...) {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("destinatário %s recusado: %w", recipient, err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("o Gmail não aceitou a mensagem: %w", err)
	}
	return client.Quit()
}

// open conecta, sobe o TLS (STARTTLS) e faz o login. O prazo vale para a
// conversa inteira, inclusive o envio que vem depois.
func (g Gmail) open(ctx context.Context, address, password string) (*smtp.Client, error) {
	addr := g.Addr
	if addr == "" {
		addr = DefaultAddr
	}
	timeout := g.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("não conectou ao servidor de e-mail %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("o servidor de e-mail não respondeu: %w", err)
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			client.Close()
			return nil, fmt.Errorf("TLS com o servidor de e-mail falhou: %w", err)
		}
	}
	// PlainAuth recusa mandar a senha sem TLS, exceto para localhost (o
	// servidor de teste): a senha nunca sai em claro pela rede.
	if err := client.Auth(smtp.PlainAuth("", address, password, host)); err != nil {
		client.Close()
		var protocol *textproto.Error
		if errors.As(err, &protocol) && protocol.Code == 535 {
			return nil, ErrLoginRefused
		}
		return nil, fmt.Errorf("login no servidor de e-mail falhou: %w", err)
	}
	return client, nil
}

// Build monta a mensagem MIME: texto em UTF-8 (quoted-printable) e os anexos
// em base64. Cabeçalho com quebra de linha é recusado — seria injeção de
// cabeçalho vinda do assunto ou do nome digitados no painel.
func Build(message ports.Mail, now time.Time) ([]byte, error) {
	for _, value := range []string{message.Subject, message.FromName, message.From} {
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("assunto, nome e remetente não podem ter quebra de linha")
		}
	}
	for _, address := range append(append([]string{message.From}, message.To...), message.CC...) {
		if _, err := mail.ParseAddress(address); err != nil {
			return nil, fmt.Errorf("e-mail inválido: %s", address)
		}
	}
	if len(message.To) == 0 {
		return nil, errors.New("e-mail sem destinatário")
	}
	var buffer bytes.Buffer
	body := multipart.NewWriter(&buffer)
	from := (&mail.Address{Name: message.FromName, Address: message.From}).String()
	headers := []string{
		"From: " + from,
		"To: " + strings.Join(message.To, ", "),
	}
	if len(message.CC) > 0 {
		headers = append(headers, "Cc: "+strings.Join(message.CC, ", "))
	}
	headers = append(headers,
		"Subject: "+mime.QEncoding.Encode("utf-8", message.Subject),
		"Date: "+now.Format(time.RFC1123Z),
		"Message-ID: "+messageID(message.From),
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="`+body.Boundary()+`"`,
	)
	var out bytes.Buffer
	out.WriteString(strings.Join(headers, "\r\n"))
	out.WriteString("\r\n\r\n")

	text, _ := body.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	encoder := quotedprintable.NewWriter(text)
	// O SMTP exige CRLF; o texto do painel chega com \n ou \r\n.
	_, _ = encoder.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(message.Body, "\r\n", "\n"), "\n", "\r\n")))
	_ = encoder.Close()

	for _, attachment := range message.Attachments {
		part, _ := body.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {mime.FormatMediaType(attachment.ContentType, map[string]string{"name": attachment.Name})},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Name})},
			"Content-Transfer-Encoding": {"base64"},
		})
		encoded := base64.StdEncoding.EncodeToString(attachment.Data)
		// Linhas de 76 caracteres: o limite do MIME para base64.
		for len(encoded) > 76 {
			_, _ = part.Write([]byte(encoded[:76] + "\r\n"))
			encoded = encoded[76:]
		}
		_, _ = part.Write([]byte(encoded + "\r\n"))
	}
	_ = body.Close()
	out.Write(buffer.Bytes())
	return out.Bytes(), nil
}

// messageID gera um Message-ID único no domínio do remetente: sem ele, alguns
// filtros de spam pontuam o e-mail contra.
func messageID(from string) string {
	random := make([]byte, 12)
	_, _ = rand.Read(random)
	domain := "nfse-digital"
	if at := strings.LastIndex(from, "@"); at >= 0 {
		domain = from[at+1:]
	}
	return "<" + hex.EncodeToString(random) + "@" + domain + ">"
}
