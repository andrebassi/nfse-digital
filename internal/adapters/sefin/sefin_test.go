package sefin

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nfse/internal/adapters/passcert"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// testCertificate gera o A1 fictício do emitente EXEMPLO (CNPJ 11222333000181).
func testCertificate(t *testing.T) passcert.Certificate {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	template := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "EXEMPLO TECNOLOGIA LTDA:11222333000181"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	leaf, _ := x509.ParseCertificate(der)
	return passcert.Certificate{
		TLS: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, Leaf: leaf, Signer: key,
		Info: domain.CertificateInfo{CNPJ: "11222333000181", NotAfter: leaf.NotAfter},
	}
}

// fakeSefin sobe um servidor TLS que exige certificado de cliente e responde
// com o status e o corpo dados. Guarda o último corpo recebido.
type fakeSefin struct {
	server   *httptest.Server
	received []byte
}

// newFakeSefin cria o servidor; o handler recebe o corpo do POST.
func newFakeSefin(t *testing.T, status int, body string) *fakeSefin {
	t.Helper()
	fake := &fakeSefin{}
	fake.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/SefinNacional/nfse" || r.Method != http.MethodPost || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "rota ou certificado errado", http.StatusTeapot)
			return
		}
		fake.received, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	fake.server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert}
	fake.server.StartTLS()
	t.Cleanup(fake.server.Close)
	return fake
}

// roots devolve a AC do servidor falso, para o cliente confiar nele.
func (f *fakeSefin) roots() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(f.server.Certificate())
	return pool
}

// packedNFSe devolve uma NFS-e mínima em gzip+base64, como a SEFIN devolveria.
func packedNFSe() string {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, _ = writer.Write([]byte(`<NFSe><infNFSe><nNFSe>42</nNFSe></infNFSe></NFSe>`))
	_ = writer.Close()
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}

// TestSubmitResponses cobre cada resposta: 201, 400 com as variações de
// formato do erro, 409, 500 e corpo que não é JSON.
func TestSubmitResponses(t *testing.T) {
	certificate := testCertificate(t)
	cases := []struct {
		name     string
		status   int
		body     string
		wantErr  error
		wantKey  string
		wantCode string
	}{
		{"autorizada", 201, `{"chaveAcesso":"35057082112223330001810000000000004226101234567890","nfseXmlGZipB64":"` + packedNFSe() + `"}`, nil, "35057082112223330001810000000000004226101234567890", ""},
		{"E0039 em lista", 400, `{"erros":[{"Codigo":"E0039","Descricao":"O município emissor..."}]}`, nil, "", "E0039"},
		{"erro minúsculo", 400, `{"Erros":[{"codigo":"E0116","descricao":"IM"}]}`, nil, "", "E0116"},
		{"erro objeto único", 400, `{"erro":{"codigo":"E0160","descricao":"Simples"}}`, nil, "", "E0160"},
		{"erro no topo", 400, `{"codigo":"E0712","descricao":"totTrib"}`, nil, "", "E0712"},
		{"corpo não JSON", 418, `algo deu errado`, nil, "", "HTTP418"},
		{"duplicada", 409, `{"erros":[{"Codigo":"E0014","Descricao":"DPS duplicada"}]}`, ErrDuplicate, "", "E0014"},
		{"fora do ar", 503, `manutenção`, ErrTransient, "", ""},
	}
	for _, c := range cases {
		fake := newFakeSefin(t, c.status, c.body)
		client := NewClient(fake.server.URL+"/SefinNacional/", certificate.TLS, fake.roots())
		result, err := client.Submit(context.Background(), "ZHBz")
		if !errors.Is(err, c.wantErr) || result.AccessKey != c.wantKey {
			t.Errorf("%s: err %v, chave %q", c.name, err, result.AccessKey)
			continue
		}
		if c.wantCode != "" && (len(result.Errors) == 0 || result.Errors[0].Code != c.wantCode) {
			t.Errorf("%s: erros %+v", c.name, result.Errors)
		}
		if c.name == "autorizada" && result.NFSeNumber != "42" {
			t.Errorf("número da NFS-e: %q", result.NFSeNumber)
		}
		var sent map[string]string
		if json.Unmarshal(fake.received, &sent) != nil || sent["dpsXmlGZipB64"] != "ZHBz" {
			t.Errorf("%s: corpo enviado %s", c.name, fake.received)
		}
	}
}

// TestSubmitBadSuccess: 2xx ilegível ou sem chave é erro, não autorização.
func TestSubmitBadSuccess(t *testing.T) {
	certificate := testCertificate(t)
	for _, body := range []string{`não json`, `{}`, `{"chaveAcesso":"x","nfseXmlGZipB64":"!!!"}`, `{"chaveAcesso":"x","nfseXmlGZipB64":"bm90LWd6aXA="}`} {
		fake := newFakeSefin(t, 201, body)
		if _, err := NewClient(fake.server.URL+"/SefinNacional", certificate.TLS, fake.roots()).Submit(context.Background(), "x"); err == nil {
			t.Errorf("corpo %q deveria falhar", body)
		}
	}
}

// TestMutualTLSIsRequired: sem certificado de cliente o servidor recusa o
// handshake — prova de que o mTLS está mesmo ligado no teste.
func TestMutualTLSIsRequired(t *testing.T) {
	fake := newFakeSefin(t, 201, `{}`)
	client := NewClient(fake.server.URL+"/SefinNacional", tls.Certificate{}, fake.roots())
	if _, err := client.Submit(context.Background(), "x"); !errors.Is(err, ErrTransient) {
		t.Fatalf("sem certificado: %v", err)
	}
	bad := NewClient("://url-invalida", testCertificate(t).TLS, nil)
	if _, err := bad.Submit(context.Background(), "x"); err == nil {
		t.Fatal("URL inválida deveria falhar")
	}
}

// TestRenegotiationAllowedOnce trava o conserto de 29/10/2026: a SEFIN pede o
// certificado por renegociação, e sem esta opção a emissão falha no TLS.
func TestRenegotiationAllowedOnce(t *testing.T) {
	client := NewClient(HomologationURL, testCertificate(t).TLS, nil)
	if client.HTTP.Transport.(*http.Transport).TLSClientConfig.Renegotiation != tls.RenegotiateOnceAsClient {
		t.Fatal("renegociação TLS desligada: a SEFIN recusa a conexão")
	}
}

// TestBaseURL confere as duas URLs oficiais.
func TestBaseURL(t *testing.T) {
	if BaseURL(domain.EnvProduction) != ProductionURL || BaseURL(domain.EnvHomologation) != HomologationURL {
		t.Fatal("URL base")
	}
	if !strings.Contains(HomologationURL, "producaorestrita") {
		t.Fatal("homologação deveria ser a produção restrita")
	}
}

// fakeLoader devolve o certificado de teste ou um erro.
type fakeLoader struct {
	certificate passcert.Certificate
	err         error
}

// Load devolve o que o teste configurou.
func (f fakeLoader) Load(context.Context) (passcert.Certificate, error) { return f.certificate, f.err }

// emitRequest monta um pedido de emissão válido do emitente EXEMPLO.
func emitRequest() ports.EmitRequest {
	return ports.EmitRequest{
		Issuer: domain.Issuer{
			LegalName: "EXEMPLO TECNOLOGIA LTDA", CNPJ: "11222333000181", CityCode: "3505708", SimplesOption: "me-epp",
			SimplesRegime: "federal-municipal-sn", SpecialRegime: "none", SimplesTaxRate: 600, Environment: domain.EnvHomologation, Series: "1", NextDPSNumber: 2,
		},
		Customer: domain.Customer{Name: "IPÊ INFORMÁTICA LTDA", Document: "56789012000100"},
		Template: domain.Template{NationalTaxCode: "010701", ServiceCityCode: "3505708"},
		Invoice: domain.Invoice{
			Environment: domain.EnvHomologation, Series: "1", DPSNumber: 1, Amount: 1000, Description: "teste",
			IssuedAt: time.Now(), Competence: time.Now(),
		},
	}
}

// TestEmitterOutcomes: autorizada, rejeitada (com todas as mensagens),
// duplicada e fora do ar, contra o servidor falso.
func TestEmitterOutcomes(t *testing.T) {
	certificate := testCertificate(t)
	cases := []struct {
		status int
		body   string
		want   domain.InvoiceStatus
		err    error
	}{
		{201, `{"chaveAcesso":"CHAVE","nfseXmlGZipB64":"` + packedNFSe() + `"}`, domain.StatusAuthorized, nil},
		{400, `{"erros":[{"Codigo":"E0039","Descricao":"municipio"},{"Codigo":"E0120","Descricao":"IM","Complemento":"x"}]}`, domain.StatusRejected, nil},
		{409, `{}`, "", ErrDuplicate},
		{502, ``, "", ErrTransient},
	}
	for _, c := range cases {
		fake := newFakeSefin(t, c.status, c.body)
		emitter := &Emitter{Certificates: fakeLoader{certificate: certificate}, AppVersion: "teste", BaseURLOverride: fake.server.URL + "/SefinNacional", Roots: fake.roots()}
		result, err := emitter.Emit(context.Background(), emitRequest())
		if !errors.Is(err, c.err) || result.Status != c.want {
			t.Errorf("HTTP %d: %+v, %v", c.status, result, err)
		}
		if c.status == 400 && (result.ErrorCode != "E0039" || !strings.Contains(result.ErrorMessage, "[E0120] IM x")) {
			t.Errorf("rejeição: %+v", result)
		}
		if !strings.Contains(string(emitter.LastSentXML), "<infDPS Id=\"DPS") {
			t.Error("XML enviado não guardado")
		}
	}
}

// TestEmitterRefusals: produção bloqueada, cadastro inválido, certificado que
// não abre e certificado de outro CNPJ param antes de chamar a SEFIN.
func TestEmitterRefusals(t *testing.T) {
	certificate := testCertificate(t)
	production := emitRequest()
	production.Invoice.Environment = domain.EnvProduction
	invalid := emitRequest()
	invalid.Customer.Document = "123"
	otherCNPJ := testCertificate(t)
	otherCNPJ.Info.CNPJ = "56789012000100"
	cases := []struct {
		name    string
		loader  fakeLoader
		request ports.EmitRequest
		want    error
	}{
		{"produção", fakeLoader{certificate: certificate}, production, ErrProductionBlocked},
		{"cadastro inválido", fakeLoader{certificate: certificate}, invalid, nil},
		{"certificado não abre", fakeLoader{err: passcert.ErrPassFailed}, emitRequest(), passcert.ErrPassFailed},
		{"outro CNPJ", fakeLoader{certificate: otherCNPJ}, emitRequest(), nil},
	}
	for _, c := range cases {
		emitter := &Emitter{Certificates: c.loader, BaseURLOverride: "https://127.0.0.1:1"}
		_, err := emitter.Emit(context.Background(), c.request)
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	broken := testCertificate(t)
	broken.Signer = failingSigner{broken.Signer}
	if _, err := (&Emitter{Certificates: fakeLoader{certificate: broken}}).Emit(context.Background(), emitRequest()); err == nil {
		t.Error("falha de assinatura deveria parar a emissão")
	}
}
