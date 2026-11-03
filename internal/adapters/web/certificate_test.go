package web

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"nfse/internal/domain"
)

// postPFX envia o formulário do certificado; pfx nil omite o arquivo.
func (f fixture) postPFX(pfx []byte, password string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("csrf", f.server.csrf)
	_ = writer.WriteField("password", password)
	if pfx != nil {
		part, _ := writer.CreateFormFile("pfx", "certificado.pfx")
		_, _ = part.Write(pfx)
	}
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/settings/certificate", &body)
	request.Host = "127.0.0.1:8735"
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(f.session)
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// certificateInfo é o certificado fictício que os fakes devolvem.
var certificateInfo = domain.CertificateInfo{HolderName: "EXEMPLO TECNOLOGIA LTDA", CNPJ: "11222333000181", NotAfter: time.Date(2027, 12, 2, 0, 0, 0, 0, time.UTC)}

// TestCertificateHiddenWithoutOps: sem o certificado ligado não há envio nem conferência.
func TestCertificateHiddenWithoutOps(t *testing.T) {
	f := newFixture(t, nil, nil)
	body := f.get("/settings/certificate").Body.String()
	if strings.Contains(body, `action="/settings/certificate"`) || !strings.Contains(body, "Nenhum certificado enviado ainda.") {
		t.Fatal("painel sem certificado mostrou envio")
	}
	if response := f.post("/settings/certificate", url.Values{}); response.Code != http.StatusNotFound {
		t.Errorf("envio sem certificado ligado: %d", response.Code)
	}
}

// TestCertificateReplace: sem arquivo, sem senha, grande demais e recusa do
// adapter não gravam; o CNPJ do emitente vai como esperado; sucesso grava os
// dados públicos e mostra a faixa "titular · CNPJ · válido até".
func TestCertificateReplace(t *testing.T) {
	f := newFixture(t, nil, nil)
	var gotExpected, gotPassword string
	refuse := false
	f.server.UseCertificate(CertificateOps{
		Replace: func(_ context.Context, pfx []byte, password, expected string) (domain.CertificateInfo, error) {
			gotExpected, gotPassword = expected, password
			if refuse {
				return domain.CertificateInfo{}, errors.New("senha do certificado incorreta: confira a senha do arquivo .pfx")
			}
			return certificateInfo, nil
		},
		Storage: "Gravado no cofre de teste.",
	})
	if body := f.get("/settings/certificate").Body.String(); !strings.Contains(body, "Arraste o certificado A1") || !strings.Contains(body, "Enviar certificado") || !strings.Contains(body, "Gravado no cofre de teste.") || strings.Contains(body, "certificado do pass") || strings.Contains(body, "cifrado no pass") {
		t.Fatal("tela sem área de envio, sem a frase de onde fica, ou ainda falando do pass")
	}
	cases := map[string]struct {
		pfx      []byte
		password string
		want     string
	}{
		"sem arquivo":   {nil, "x", "escolha o arquivo"},
		"sem senha":     {[]byte("pfx"), "", "informe a senha"},
		"grande demais": {bytes.Repeat([]byte("x"), maxPFXBytes+1), "x", "grande demais"},
	}
	for name, c := range cases {
		response := f.postPFX(c.pfx, c.password)
		if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), c.want) {
			t.Errorf("%s: %d", name, response.Code)
		}
	}
	refuse = true
	refused := f.postPFX([]byte("pfx"), "senha-secreta-123")
	if refused.Code != http.StatusUnprocessableEntity || strings.Contains(refused.Body.String(), "senha-secreta-123") {
		t.Fatal("recusa do adapter: código errado ou a senha voltou na página")
	}
	if status, _ := f.repo.CertificateStatus(context.Background()); status.Info.CNPJ != "" {
		t.Fatal("recusa gravou o certificado")
	}
	refuse = false
	accepted := f.postPFX([]byte("pfx"), "senha-secreta-123")
	if accepted.Code != http.StatusSeeOther || gotExpected != "11222333000181" || gotPassword != "senha-secreta-123" {
		t.Fatalf("envio: %d, esperado %q", accepted.Code, gotExpected)
	}
	body := f.get(accepted.Header().Get("Location")).Body.String()
	if !strings.Contains(body, "EXEMPLO TECNOLOGIA LTDA · <span class=\"font-mono\">11.222.333/0001-81</span> · válido até 02/12/2027") || !strings.Contains(body, "Substituir certificado") || !strings.Contains(body, "Certificado de EXEMPLO TECNOLOGIA LTDA gravado") {
		t.Fatalf("faixa do certificado: %s", body)
	}
}

// TestCertificatePortal: o envio grava os dados públicos; autorizar e
// revogar a consulta ao portal mantêm o certificado.
func TestCertificatePortal(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.server.UseCertificate(CertificateOps{Replace: func(context.Context, []byte, string, string) (domain.CertificateInfo, error) {
		return certificateInfo, nil
	}})
	f.postPFX([]byte("pfx"), "senha-secreta-123")
	ctx := context.Background()
	if status, _ := f.repo.CertificateStatus(ctx); status.Info.CNPJ != "11222333000181" || status.CheckedAt.IsZero() {
		t.Fatalf("conferência não gravou: %+v", status)
	}
	if body := f.get("/settings/certificate").Body.String(); !strings.Contains(body, "Autorizar consulta") {
		t.Fatal("portal deveria começar não autorizado")
	}
	f.post("/settings/portal", url.Values{"action": {"authorize"}})
	status, _ := f.repo.CertificateStatus(ctx)
	if !status.PortalAuthorized() || status.Info.CNPJ == "" {
		t.Fatalf("autorizar: %+v", status)
	}
	if body := f.get("/settings/certificate").Body.String(); !strings.Contains(body, "Revogar autorização") || !strings.Contains(body, "Autorizada em 29/10/2026") {
		t.Fatal("tela não mostra a autorização")
	}
	f.post("/settings/portal", url.Values{"action": {"revoke"}})
	if status, _ := f.repo.CertificateStatus(ctx); status.PortalAuthorized() || status.Info.CNPJ == "" {
		t.Fatalf("revogar: %+v", status)
	}
}
