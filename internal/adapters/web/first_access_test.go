package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"nfse/internal/domain"
)

// TestFirstAccessOnlyCertificate: conta sem certificado só abre a aba
// Certificado — qualquer outra tela (e qualquer POST) volta para ela, o menu
// e as outras abas aparecem desabilitados, o ambiente não troca, e o botão
// antigo de conferir o pass não existe. Depois do envio, o painel abre.
func TestFirstAccessOnlyCertificate(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.server.UseCertificate(CertificateOps{Replace: func(context.Context, []byte, string, string) (domain.CertificateInfo, error) {
		return certificateInfo, nil
	}})
	for _, path := range []string{"/invoices", "/emit", "/settings", "/settings/security", "/integrations", "/customers"} {
		response := f.get(path)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/settings/certificate" {
			t.Errorf("GET %s: %d → %s", path, response.Code, response.Header().Get("Location"))
		}
	}
	if response := f.post("/customers", url.Values{"name": {"X"}}); response.Header().Get("Location") != "/settings/certificate" {
		t.Errorf("POST passou sem certificado: %d", response.Code)
	}

	page := f.get("/settings/certificate")
	body := page.Body.String()
	if page.Code != http.StatusOK {
		t.Fatalf("aba Certificado: %d", page.Code)
	}
	for _, want := range []string{
		`<span class="tab tab-disabled" aria-disabled="true">Emitente</span>`,
		`<span class="tab tab-disabled" aria-disabled="true">Segurança</span>`,
		`nav-disabled"`, `env-locked`, "Envie o certificado A1 para começar",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("travado sem %q", want)
		}
	}
	for _, gone := range []string{"/settings/certificate/check", "Conferir o certificado do pass", "protótipo"} {
		if strings.Contains(body, gone) {
			t.Errorf("ainda mostra %q", gone)
		}
	}

	f.postPFX([]byte("pfx"), "senha-secreta-123")
	opened := f.get("/invoices")
	if opened.Code != http.StatusOK || strings.Contains(opened.Body.String(), "nav-disabled") || strings.Contains(opened.Body.String(), "env-locked") {
		t.Fatalf("depois do certificado o painel continua travado: %d", opened.Code)
	}
	if body := f.get("/settings/certificate").Body.String(); strings.Contains(body, "tab-disabled") {
		t.Fatal("abas continuam travadas depois do certificado")
	}
}
