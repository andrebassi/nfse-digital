package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// webMailer guarda os e-mails; verifyErr e sendErr simulam o Gmail recusando.
type webMailer struct {
	sent      []ports.Mail
	verifyErr error
	sendErr   error
}

// Verify cumpre ports.Mailer para o teste.
func (m *webMailer) Verify(context.Context, string, string) error { return m.verifyErr }

// Send cumpre ports.Mailer para o teste.
func (m *webMailer) Send(_ context.Context, _ string, mail ports.Mail) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, mail)
	return nil
}

// plainSealer guarda com um prefixo (o teste não é sobre cifra).
type plainSealer struct{}

// Seal cumpre ports.SecretSealer para o teste.
func (plainSealer) Seal(_ context.Context, plain []byte) ([]byte, error) {
	return append([]byte("selado:"), plain...), nil
}

// Open cumpre ports.SecretSealer para o teste.
func (plainSealer) Open(_ context.Context, sealed []byte) ([]byte, error) {
	return []byte(strings.TrimPrefix(string(sealed), "selado:")), nil
}

// webDocuments devolve anexos fixos.
type webDocuments struct{}

// Invoice cumpre ports.InvoiceDocuments para o teste.
func (webDocuments) Invoice(context.Context, domain.Invoice) ([]byte, []byte, error) {
	return []byte("%PDF"), []byte("<NFSe/>"), nil
}

// Sample cumpre ports.InvoiceDocuments para o teste.
func (webDocuments) Sample(context.Context, domain.Issuer, domain.Customer, domain.Invoice) ([]byte, []byte, error) {
	return []byte("%PDF simulação"), []byte("<simulacao/>"), nil
}

// enableEmail liga o envio por e-mail no painel de teste.
func enableEmail(f fixture) *webMailer {
	mailer := &webMailer{}
	f.server.service.Mailer, f.server.service.Sealer, f.server.service.Documents = mailer, plainSealer{}, webDocuments{}
	return mailer
}

// emailValues é o formulário da aba preenchido como o dono faria.
func emailValues(action string) url.Values {
	return url.Values{
		"action": {action}, "sender_address": {"dono@gmail.example"}, "sender_name": {"Financeiro"},
		"sender_password": {"abcd efgh ijkl mnop"}, "to": {"nf@cliente.example"}, "cc": {""},
		"subject": {"NFS-e nº {numero} - {emitente}"}, "body": {"Olá {tomador},\r\nvalor {valor}"},
		"attach_pdf": {"on"}, "attach_xml": {"on"}, "test_to": {"eu@exemplo.example"},
	}
}

// TestEmailDisabled: sem envio ligado, a aba, o botão da nota e as rotas não existem.
func TestEmailDisabled(t *testing.T) {
	f := newFixture(t, nil, nil)
	ipe := itoa(f.customerID(t, "ipe"))
	if body := f.get("/customers/" + ipe).Body.String(); strings.Contains(body, "Envio por e-mail") {
		t.Fatal("aba de e-mail apareceu desligada")
	}
	for path, response := range map[string]*httptest.ResponseRecorder{
		"GET aba":      f.get("/customers/" + ipe + "/email"),
		"POST aba":     f.post("/customers/"+ipe+"/email", emailValues("save")),
		"POST prévia":  f.post("/customers/"+ipe+"/email/preview", emailValues("")),
		"POST da nota": f.post("/invoices/1/email", url.Values{}),
	} {
		if response.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, response.Code)
		}
	}
}

// TestCustomerEmailTab: a aba abre com o padrão (Para = e-mail do cadastro,
// marcadores, prévia com a nota 123); salvar sem senha volta com o erro no
// campo; com senha, grava e redireciona; o Gmail recusando volta na tela; a
// senha nunca é devolvida no HTML.
func TestCustomerEmailTab(t *testing.T) {
	f := newFixture(t, nil, nil)
	mailer := enableEmail(f)
	id := f.customerID(t, "ipe")
	customer, _ := f.repo.Customer(context.Background(), id)
	path := "/customers/" + itoa(id) + "/email"
	if body := f.get("/customers/" + itoa(id)).Body.String(); !strings.Contains(body, `href="`+path+`"`) {
		t.Fatal("cadastro sem a aba Envio por e-mail")
	}
	page := f.get(path).Body.String()
	for _, want := range []string{`value="` + customer.Email + `"`, `data-placeholder="{mesCompetencia}"`, "NFS-e nº 123 - ", "danfse-123.pdf", "dona@exemplo.example", "/static/email.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("aba sem %q", want)
		}
	}
	noPassword := emailValues("save")
	noPassword.Del("sender_password")
	if response := f.post(path, noPassword); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "informe a senha de app") {
		t.Fatalf("sem senha: %d", response.Code)
	}
	mailer.verifyErr = errors.New("o Gmail recusou o login")
	refused := f.post(path, emailValues("save"))
	if refused.Code != http.StatusUnprocessableEntity || !strings.Contains(refused.Body.String(), "o Gmail recusou o login") || strings.Contains(refused.Body.String(), `value="abcd efgh ijkl mnop"`) {
		t.Fatalf("login recusado: %d (senha devolvida?)", refused.Code)
	}
	mailer.verifyErr = nil
	saved := f.post(path, emailValues("save"))
	if saved.Code != http.StatusSeeOther || !strings.Contains(saved.Header().Get("Location"), "Envio+por+e-mail+salvo") {
		t.Fatalf("salvar: %d", saved.Code)
	}
	after := f.get(path).Body.String()
	if !strings.Contains(after, "guardada — deixe vazio para manter") || !strings.Contains(after, "Olá {tomador},\nvalor {valor}") {
		t.Fatal("tela depois de salvar sem a senha guardada ou sem o texto")
	}
	bad := emailValues("save")
	bad.Set("to", "não é e-mail")
	if response := f.post(path, bad); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "e-mail inválido") {
		t.Fatalf("Para inválido: %d", response.Code)
	}
	if response := f.get("/customers/99999/email"); response.Code != http.StatusNotFound {
		t.Fatalf("tomador inexistente: %d", response.Code)
	}
}

// TestCustomerEmailTestAndPreview: "Enviar teste" manda o texto da tela com
// [TESTE] e nada é salvo; falha do Gmail volta como mensagem; a prévia
// devolve só o cartão, com os marcadores resolvidos.
func TestCustomerEmailTestAndPreview(t *testing.T) {
	f := newFixture(t, nil, nil)
	mailer := enableEmail(f)
	id := f.customerID(t, "ipe")
	customer, _ := f.repo.Customer(context.Background(), id)
	path := "/customers/" + itoa(id) + "/email"
	response := f.post(path, emailValues("test"))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "E-mail de teste enviado para eu@exemplo.example") || len(mailer.sent) != 1 ||
		!strings.HasPrefix(mailer.sent[0].Subject, "[TESTE] NFS-e nº 123") {
		t.Fatalf("teste: %d %+v", response.Code, mailer.sent)
	}
	if _, err := f.repo.CustomerEmail(context.Background(), id); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("o teste salvou o envio")
	}
	mailer.sendErr = errors.New("Gmail fora do ar")
	if response := f.post(path, emailValues("test")); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "Gmail fora do ar") {
		t.Fatalf("teste com Gmail fora: %d", response.Code)
	}
	preview := emailValues("")
	preview.Set("subject", "Oi {tomador}")
	preview.Del("attach_xml")
	body := f.post(path+"/preview", preview).Body.String()
	if !strings.Contains(body, "Oi "+customer.Name) || strings.Contains(body, "nfse-123.xml") || strings.Contains(body, "<html") {
		t.Fatalf("prévia:\n%s", body)
	}
}

// TestInvoiceEmailButton: a nota autorizada tem o botão; enviar sem
// remetente volta com o motivo e registra a falha; com remetente, envia e o
// histórico mostra "Enviada · manual"; nota inexistente é 404.
func TestInvoiceEmailButton(t *testing.T) {
	f := newFixture(t, nil, nil)
	mailer := enableEmail(f)
	id := saveAuthorized(t, f, domain.EnvHomologation)
	detail := detailPath(id)
	if !strings.Contains(f.get(detail).Body.String(), "Enviar por e-mail") {
		t.Fatal("nota autorizada sem o botão")
	}
	failed := f.post("/invoices/"+itoa(id)+"/email", url.Values{})
	if failed.Code != http.StatusSeeOther || !strings.Contains(failed.Header().Get("Location"), url.QueryEscape("E-mail não enviado")) {
		t.Fatalf("sem remetente: %d %q", failed.Code, failed.Header().Get("Location"))
	}
	customer := f.customerID(t, "")
	invoice, _ := f.repo.Invoice(context.Background(), id)
	values := emailValues("save")
	if response := f.post("/customers/"+itoa(invoice.CustomerID)+"/email", values); response.Code != http.StatusSeeOther {
		t.Fatalf("configurar remetente: %d (tomador %d)", response.Code, customer)
	}
	sent := f.post("/invoices/"+itoa(id)+"/email", url.Values{})
	if !strings.Contains(sent.Header().Get("Location"), url.QueryEscape("E-mail enviado para nf@cliente.example")) || len(mailer.sent) != 1 {
		t.Fatalf("enviar: %q", sent.Header().Get("Location"))
	}
	history := f.get(detail).Body.String()
	if !strings.Contains(history, "Envios por e-mail") || !strings.Contains(history, "manual · nf@cliente.example") || !strings.Contains(history, "Falhou") {
		t.Fatal("histórico sem os dois envios")
	}
	if response := f.post("/invoices/99999/email", url.Values{}); response.Code != http.StatusNotFound {
		t.Fatalf("inexistente: %d", response.Code)
	}
}
