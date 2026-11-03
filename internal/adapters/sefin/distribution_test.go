package sefin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rootsOf devolve a AC do servidor de teste, para o leitor confiar nele.
func rootsOf(server *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	return pool
}

// newFakeADN sobe um servidor TLS que exige certificado e responde por NSU.
func newFakeADN(t *testing.T, responses map[string]struct {
	status int
	body   string
}) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "só GET com certificado", http.StatusTeapot)
			return
		}
		if r.URL.Path == "/redireciona" {
			http.Redirect(w, r, "http://exemplo.example/docs", http.StatusMovedPermanently)
			return
		}
		response := responses[r.URL.Path]
		w.WriteHeader(response.status)
		_, _ = io.WriteString(w, response.body)
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

// TestDistribution cobre lote com documentos, fim por E2220, fim por 404 sem
// erro, recusa com erro, corpo ilegível e documento ilegível.
func TestDistribution(t *testing.T) {
	certificate := testCertificate(t)
	server := newFakeADN(t, map[string]struct {
		status int
		body   string
	}{
		"/contribuintes/DFe/0": {200, `{"StatusProcessamento":"DOCUMENTOS_LOCALIZADOS","LoteDFe":[{"NSU":1,"ChaveAcesso":"K","TipoDocumento":"NFSE","ArquivoXml":"` + packedNFSe() + `"}]}`},
		"/contribuintes/DFe/1": {404, `{"StatusProcessamento":"NENHUM_DOCUMENTO_LOCALIZADO","Erros":[{"Codigo":"E2220","Descricao":"Nenhum documento"}]}`},
		"/contribuintes/DFe/2": {404, `{}`},
		"/contribuintes/DFe/3": {400, `{"Erros":[{"Codigo":"E9999","Descricao":"recusado"}]}`},
		"/contribuintes/DFe/4": {500, `não json`},
		"/contribuintes/DFe/5": {200, `{"StatusProcessamento":"DOCUMENTOS_LOCALIZADOS","LoteDFe":[{"NSU":6,"ArquivoXml":"!!!"}]}`},
	})
	distributor := Distributor{Reader: NewReader(certificate.TLS, rootsOf(server)), BaseURL: server.URL}
	batch, err := distributor.Distribution(context.Background(), 0)
	if err != nil || !batch.More || len(batch.Documents) != 1 || !strings.Contains(string(batch.Documents[0].XML), "<nNFSe>42</nNFSe>") {
		t.Fatalf("lote: %+v, %v", batch, err)
	}
	for _, nsu := range []int64{1, 2} {
		if batch, err := distributor.Distribution(context.Background(), nsu); err != nil || batch.More || len(batch.Documents) != 0 {
			t.Errorf("fim (NSU %d): %+v, %v", nsu, batch, err)
		}
	}
	for _, nsu := range []int64{3, 4, 5} {
		if _, err := distributor.Distribution(context.Background(), nsu); err == nil {
			t.Errorf("NSU %d deveria falhar", nsu)
		}
	}
}

// TestReaderRefusesPlainHTTPAndRedirects: só https, e redirecionamento não é
// seguido (a SEFIN redireciona rota errada para http).
func TestReaderRefusesPlainHTTPAndRedirects(t *testing.T) {
	certificate := testCertificate(t)
	server := newFakeADN(t, nil)
	reader := NewReader(certificate.TLS, rootsOf(server))
	if _, err := reader.Get(context.Background(), "http://exemplo.example"); err == nil {
		t.Fatal("http deveria ser recusado")
	}
	result, err := reader.Get(context.Background(), server.URL+"/redireciona")
	if err != nil || result.StatusCode != http.StatusMovedPermanently || result.Location != "http://exemplo.example/docs" {
		t.Fatalf("redirecionamento: %+v, %v", result, err)
	}
	if _, err := reader.Get(context.Background(), "https://127.0.0.1:1/x"); err == nil {
		t.Fatal("porta fechada deveria falhar")
	}
	if ADNBaseURL("production") != ProductionADNURL || ADNBaseURL("homologation") != HomologationADNURL {
		t.Fatal("URL do ADN")
	}
}
