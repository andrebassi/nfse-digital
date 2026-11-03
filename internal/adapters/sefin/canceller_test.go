package sefin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nfse/internal/adapters/passcert"
	"nfse/internal/domain"
)

// cancelKey é a chave de acesso fictícia usada nos testes de evento.
const cancelKey = "35057082112223330001810000000000003826100000000001"

// newFakeEvents sobe um servidor TLS que só aceita POST com certificado na
// rota de eventos da chave fictícia, e guarda o corpo recebido.
func newFakeEvents(t *testing.T, status int, body string, received *[]byte) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nfse/"+cancelKey+"/eventos" || r.Method != http.MethodPost || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "rota ou certificado errado", http.StatusTeapot)
			return
		}
		*received, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

// cancelEvent é um pedido fictício já validado pelo domínio.
func cancelEvent(environment domain.Environment) domain.CancelEvent {
	return domain.CancelEvent{
		ID: "PRE" + cancelKey + domain.CancelEventType, Environment: environment, AppVersion: "teste",
		At: time.Now(), AuthorCNPJ: "11222333000181", AccessKey: cancelKey, ReasonCode: "1", Description: "valor digitado errado",
	}
}

// TestCancellerOutcomes: registrado (com e sem XML de volta), recusado com
// lista de erro, recusado sem corpo legível e fora do ar.
func TestCancellerOutcomes(t *testing.T) {
	certificate := testCertificate(t)
	cases := []struct {
		status     int
		body       string
		registered bool
		code       string
		err        error
	}{
		{201, `{"eventoXmlGZipB64":"` + packedNFSe() + `"}`, true, "", nil},
		{200, `{}`, true, "", nil},
		{400, `{"erros":[{"codigo":"E1840","descricao":"Prazo de cancelamento expirado"}]}`, false, "E1840", nil},
		// Formato real medido em 30/10/2026: lista em "erro", singular.
		{400, `{"tipoAmbiente":2,"erro":[{"codigo":"E1831","descricao":"a NFS-e indicada não existe"}]}`, false, "E1831", nil},
		{404, `não achei`, false, "HTTP404", nil},
		{200, `{"erros":[{"codigo":"E9","descricao":"recusado com 200"}]}`, false, "E9", nil},
		{503, ``, false, "", ErrTransient},
	}
	for _, c := range cases {
		var received []byte
		server := newFakeEvents(t, c.status, c.body, &received)
		canceller := &Canceller{Certificates: fakeLoader{certificate: certificate}, BaseURLOverride: server.URL, Roots: rootsOf(server)}
		result, err := canceller.Cancel(context.Background(), cancelEvent(domain.EnvHomologation))
		if !errors.Is(err, c.err) || result.Registered != c.registered || result.ErrorCode != c.code {
			t.Errorf("HTTP %d: %+v, %v", c.status, result, err)
		}
		var payload map[string]string
		if json.Unmarshal(received, &payload) != nil || payload[EventField] == "" {
			t.Errorf("HTTP %d: corpo enviado sem %s: %s", c.status, EventField, received)
		}
		if !strings.Contains(string(canceller.LastSentXML), `<infPedReg Id="PRE`) {
			t.Error("pedido enviado não guardado")
		}
		if c.status == 201 && !strings.Contains(string(result.EventXML), "<nNFSe>42") {
			t.Errorf("XML do evento: %s", result.EventXML)
		}
	}
}

// TestCancellerRefusals: produção bloqueada, certificado que não abre,
// certificado de outro CNPJ e assinatura que falha param antes de enviar.
func TestCancellerRefusals(t *testing.T) {
	certificate := testCertificate(t)
	otherCNPJ := testCertificate(t)
	otherCNPJ.Info.CNPJ = "56789012000100"
	brokenSigner := testCertificate(t)
	brokenSigner.Signer = failingSigner{brokenSigner.Signer}
	cases := []struct {
		name   string
		loader fakeLoader
		env    domain.Environment
		want   error
	}{
		{"produção", fakeLoader{certificate: certificate}, domain.EnvProduction, ErrProductionBlocked},
		{"certificado não abre", fakeLoader{err: passcert.ErrPassFailed}, domain.EnvHomologation, passcert.ErrPassFailed},
		{"outro CNPJ", fakeLoader{certificate: otherCNPJ}, domain.EnvHomologation, nil},
		{"assinatura falha", fakeLoader{certificate: brokenSigner}, domain.EnvHomologation, nil},
	}
	for _, c := range cases {
		canceller := &Canceller{Certificates: c.loader, BaseURLOverride: "https://127.0.0.1:1"}
		_, err := canceller.Cancel(context.Background(), cancelEvent(c.env))
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// Sem override, vai à URL do ambiente; com a rede apontada para lugar
	// nenhum o resultado é ErrTransient, nunca um registro.
	offline := &Canceller{Certificates: fakeLoader{certificate: certificate}, BaseURLOverride: "https://127.0.0.1:1"}
	if _, err := offline.Cancel(context.Background(), cancelEvent(domain.EnvHomologation)); !errors.Is(err, ErrTransient) {
		t.Fatalf("rede fora: %v", err)
	}
	if _, err := (&Client{BaseURL: "://"}).SubmitEvent(context.Background(), cancelKey, "x"); err == nil {
		t.Fatal("URL inválida deveria falhar")
	}
}
