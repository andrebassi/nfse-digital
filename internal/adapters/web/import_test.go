package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// referenceNFSe lê a NFS-e fictícia de testdata/.
func referenceNFSe(t *testing.T) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "nfse-reference.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// postFile envia o formulário multipart de importação.
func (f fixture) postFile(fields map[string]string, content []byte, size int) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("csrf", f.server.csrf)
	for key, value := range fields {
		_ = writer.WriteField(key, value)
	}
	if content != nil {
		part, _ := writer.CreateFormFile("xml", "nota.xml")
		_, _ = part.Write(content)
	}
	if size > 0 {
		_ = writer.WriteField("enchimento", strings.Repeat("x", size))
	}
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/templates/import", &body)
	request.Host = "127.0.0.1:8735"
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(f.session)
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// TestImportPage: a tela abre; o XML de referência importa tomador, modelo e
// nota; sem arquivo, com arquivo errado, com outro emitente e acima do limite
// a tela explica o motivo.
func TestImportPage(t *testing.T) {
	f := newFixture(t, nil, nil)
	if response := f.get("/templates/import"); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Importar de um XML") {
		t.Fatalf("tela: %d", response.Code)
	}
	ok := f.postFile(map[string]string{"template_name": "Ipê importado"}, referenceNFSe(t), 0)
	if body := ok.Body.String(); ok.Code != http.StatusOK || !strings.Contains(body, "importada: nº 38") || !strings.Contains(body, "Ipê importado") {
		t.Fatalf("importar: %d %s", ok.Code, body)
	}
	cases := map[string]*httptest.ResponseRecorder{
		"escolha o arquivo XML": f.postFile(nil, nil, 0),
		"não é um XML de NFS-e": f.postFile(nil, []byte("<html/>"), 0),
		"envio grande demais":   f.postFile(nil, referenceNFSe(t), maxPostSize),
	}
	for want, response := range cases {
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("faltou %q: %d %s", want, response.Code, response.Body.String())
		}
	}
	// O emitente demo é EXEMPLO (11.222.333/0001-81), o mesmo do XML: trocar o
	// CNPJ do XML faz a importação recusar outro emitente.
	other := bytes.ReplaceAll(referenceNFSe(t), []byte("11222333000181"), []byte("34567890000130"))
	if response := f.postFile(nil, other, 0); response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "outro emitente") {
		t.Errorf("outro emitente: %d", response.Code)
	}
}
