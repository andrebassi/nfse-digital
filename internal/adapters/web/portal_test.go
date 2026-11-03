package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestPortalHiddenWithoutOps: sem as leituras ligadas (painel sem SEFIN), menu,
// botão e rotas não existem.
func TestPortalHiddenWithoutOps(t *testing.T) {
	f := newFixture(t, nil, nil)
	if body := f.get("/invoices").Body.String(); strings.Contains(body, "Importar do Portal Nacional") || strings.Contains(body, "Sincronizar") {
		t.Fatal("painel sem SEFIN mostrou portal ou sync")
	}
	for _, path := range []string{"/portal/import", "/sync"} {
		if response := f.post(path, url.Values{}); response.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, response.Code)
		}
	}
	if response := f.get("/portal"); response.Code != http.StatusNotFound {
		t.Errorf("/portal: %d", response.Code)
	}
}

// TestPortalImportAndSync: importar lê PRODUÇÃO e grava tudo de uma vez;
// sem autorização recusa; o resultado vem como página inteira ou como o
// fragmento que o portal.js injeta; importar de novo não grava nada (é
// idempotente); falha da base nacional vira 502 com o motivo no cartão;
// sincronizar volta para a lista com o resumo (ou com o erro).
func TestPortalImportAndSync(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	customers, _ := f.repo.Customers(ctx, "")
	templates, _ := f.repo.Templates(ctx, "")
	note := domain.ImportedNFSe{
		Issuer:   memoryIssuer(ctx, f),
		Customer: customers[0],
		Template: templates[0],
		Invoice:  domain.Invoice{Environment: domain.EnvProduction, Status: domain.StatusAuthorized, AccessKey: "KPORTAL", NFSeNumber: "77", Amount: 100, IssuedAt: f.server.now(), Competence: f.server.now()},
	}
	scanErr, syncErr := error(nil), error(nil)
	var scannedEnv domain.Environment
	f.server.UsePortal(PortalOps{
		Scan: func(ctx context.Context, environment domain.Environment) (app.PortalScan, error) {
			scannedEnv = environment
			if scanErr != nil {
				return app.PortalScan{}, scanErr
			}
			return f.server.service.ScanPortal(ctx, onePage{}, app.PortalParsers{
				NFSe:  func([]byte) (domain.ImportedNFSe, error) { return note, nil },
				Event: func([]byte) (string, bool, error) { return "", false, nil },
			}, note.Issuer.CNPJ)
		},
		Sync: func(context.Context, domain.Environment) (app.SyncReport, error) {
			return app.SyncReport{Checked: 3, Cancelled: 1}, syncErr
		},
	})

	if body := f.get("/portal").Body.String(); !strings.Contains(body, "Consultando o Portal Nacional") || !strings.Contains(body, "data-portal-autostart") {
		t.Fatal("/portal deveria abrir já consultando")
	}
	// Sem autorização de consulta ao portal, importar e sincronizar recusam.
	if response := f.post("/portal/import", url.Values{}); response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "não está autorizada") {
		t.Fatalf("importar sem autorização: %d", response.Code)
	}
	if location, _ := url.QueryUnescape(f.post("/sync", url.Values{}).Header().Get("Location")); !strings.Contains(location, "não está autorizada") {
		t.Fatalf("sync sem autorização: %s", location)
	}
	_ = f.repo.SaveCertificateStatus(ctx, domain.CertificateStatus{PortalAuthorizedAt: f.server.now()})
	page := f.post("/portal/import", url.Values{})
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Consulta concluída") || !strings.Contains(page.Body.String(), "Ver as 1 notas") || scannedEnv != domain.EnvProduction {
		t.Fatalf("importar: %d %s", page.Code, scannedEnv)
	}
	if !strings.Contains(page.Body.String(), "nº 77") || !strings.Contains(page.Body.String(), customers[0].Name) || !strings.Contains(page.Body.String(), "1 lote") {
		t.Fatal("cartão sem a nota encontrada ou sem os lotes")
	}
	invoices, _ := f.repo.Invoices(ctx)
	before := len(invoices)
	fragment := f.fragment("/portal/import")
	if fragment.Code != http.StatusOK || strings.Contains(fragment.Body.String(), "<html") || !strings.Contains(fragment.Body.String(), "data-portal-result") {
		t.Fatalf("fragmento: %d", fragment.Code)
	}
	if invoices, _ = f.repo.Invoices(ctx); len(invoices) != before {
		t.Fatalf("importar de novo gravou %d nota(s) a mais", len(invoices)-before)
	}

	scanErr = errors.New("ADN fora")
	if response := f.fragment("/portal/import"); response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "ADN fora") {
		t.Fatalf("falha na consulta: %d", response.Code)
	}

	// O banco tem notas de homologação (dados fictícios dos testes) e de produção (importada
	// acima): o sync roda nos dois ambientes e soma — 3 + 3 consultadas.
	synced := f.post("/sync", url.Values{})
	location, _ := url.QueryUnescape(synced.Header().Get("Location"))
	if synced.Code != http.StatusSeeOther || !strings.Contains(location, "6 consultadas") || !strings.Contains(location, "2 canceladas") {
		t.Fatalf("sync: %d %s", synced.Code, location)
	}
	syncErr = errors.New("SEFIN fora")
	if location := f.post("/sync", url.Values{}).Header().Get("Location"); !strings.Contains(location, "Falha+ao+sincronizar") {
		t.Fatalf("sync com falha: %s", location)
	}
}

// onePage é a distribuição falsa: um lote com uma NFS-e só.
type onePage struct{}

// Distribution devolve o lote único.
func (onePage) Distribution(context.Context, int64) (ports.DFeBatch, error) {
	return ports.DFeBatch{Documents: []ports.DFeDocument{{NSU: 1, AccessKey: "KPORTAL", Type: "NFSE", XML: []byte("<x/>")}}}, nil
}

// memoryIssuer devolve o emitente fictício dos testes.
func memoryIssuer(ctx context.Context, f fixture) domain.Issuer {
	issuer, _ := f.repo.Issuer(ctx)
	return issuer
}

// fragment faz o POST do portal.js: CSRF no header e o pedido do fragmento.
func (f fixture) fragment(target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, target, nil)
	request.Host = "127.0.0.1:8735"
	request.Header.Set("X-CSRF-Token", f.server.csrf)
	request.Header.Set("X-Fragment", "portal_result")
	request.AddCookie(f.session)
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// TestCertificateStartsImport: o primeiro certificado autoriza a leitura e
// leva para /portal (importa sozinho); substituir também leva, se a consulta
// segue autorizada; com a consulta revogada, substituir não reautoriza.
func TestCertificateStartsImport(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	f.server.UseCertificate(CertificateOps{Replace: func(context.Context, []byte, string, string) (domain.CertificateInfo, error) {
		return certificateInfo, nil
	}})
	f.server.UsePortal(PortalOps{})
	if location := f.postPFX([]byte("pfx"), "senha-secreta-123").Header().Get("Location"); location != "/portal" {
		t.Fatalf("primeiro certificado: %s", location)
	}
	if status, _ := f.repo.CertificateStatus(ctx); !status.PortalAuthorized() {
		t.Fatal("o primeiro certificado deveria autorizar a leitura do portal")
	}
	if location := f.postPFX([]byte("pfx"), "senha-secreta-123").Header().Get("Location"); location != "/portal" {
		t.Fatalf("substituir: %s", location)
	}
	f.post("/settings/portal", url.Values{"action": {"revoke"}})
	if location := f.postPFX([]byte("pfx"), "senha-secreta-123").Header().Get("Location"); location == "/portal" {
		t.Fatal("substituir com a consulta revogada importou mesmo assim")
	}
	if status, _ := f.repo.CertificateStatus(ctx); status.PortalAuthorized() {
		t.Fatal("substituir reautorizou a consulta revogada")
	}
}
