package danfse

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"nfse/internal/adapters/nfsexml"
)

// reference lê a NFS-e fictícia de testdata/ e extrai os campos do DANFSe.
func reference(t *testing.T) nfsexml.DANFSeData {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "nfse-reference.xml"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := nfsexml.ParseDANFSe(data)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// TestRenderOnePagePDF: o DANFSe sai como PDF de uma página só (exigência da
// NT-008) e o QR Code aponta para a consulta pública da chave.
func TestRenderOnePagePDF(t *testing.T) {
	data := reference(t)
	pdf, err := Render(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal("não é PDF")
	}
	if pages := bytes.Count(pdf, []byte("/Type /Page\n")) + bytes.Count(pdf, []byte("/Type /Page>")) + bytes.Count(pdf, []byte("/Type /Page ")); pages != 1 {
		t.Fatalf("páginas: %d", pages)
	}
}

// TestRenderWithEmptyFields: nota mínima (sem e-mail, sem endereço, sem
// IBS/CBS, datas zeradas) ainda gera o documento, com travessões.
func TestRenderWithEmptyFields(t *testing.T) {
	pdf, err := Render(nfsexml.DANFSeData{AccessKey: "123", Number: "1", Description: strings.Repeat("texto longo ", 100)})
	if err != nil || len(pdf) == 0 {
		t.Fatalf("nota mínima: %v", err)
	}
}

// TestRenderWatermark: com marca, o PDF ganha transparência (o estado
// gráfico da marca) e fica maior que o mesmo documento sem ela; sem marca,
// nenhuma transparência entra no documento real.
func TestRenderWatermark(t *testing.T) {
	data := nfsexml.DANFSeData{AccessKey: "123", Number: "1"}
	plain, _ := Render(data)
	data.Watermark = "SIMULAÇÃO · SEM VALOR FISCAL"
	marked, err := Render(data)
	if err != nil || len(marked) <= len(plain) || !strings.Contains(string(marked), "/ca 0.25") || strings.Contains(string(plain), "/ca 0.25") {
		t.Fatalf("marca d'água: %v, %d vs %d bytes", err, len(marked), len(plain))
	}
}

// TestFileName: nome padrão com o tomador sem acento, cortado, e sem tomador.
func TestFileName(t *testing.T) {
	cases := map[string]nfsexml.DANFSeData{
		"nfse-38-acao-servicos-ltda.pdf":             {Number: "38", Taker: nfsexml.DANFSeParty{Name: "  Ação & Serviços LTDA. "}},
		"nfse-7.pdf":                                 {Number: "7"},
		"nfse-1-" + strings.Repeat("a", 39) + ".pdf": {Number: "1", Taker: nfsexml.DANFSeParty{Name: strings.Repeat("a", 39) + " b"}},
	}
	for want, data := range cases {
		if got := FileName(data); got != want {
			t.Errorf("FileName(%q) = %q, quer %q", data.Taker.Name, got, want)
		}
	}
}

// TestHelpers confere a formatação usada nos quadros.
func TestHelpers(t *testing.T) {
	if groupKey("12345678") != "1234 5678" || groupKey("123456789") != "1234 5678 9" {
		t.Error("groupKey")
	}
	if money("19000.00") != "R$ 19.000,00" || money("") != "—" || money("x") != "—" {
		t.Error("money")
	}
	if dash(" ") != "—" || dash("a") != "a" || clip("abcdef", 4) != "abc…" || clip("ab", 4) != "ab" {
		t.Error("dash/clip")
	}
	when := time.Date(2026, 10, 5, 22, 22, 6, 0, time.UTC)
	if date(time.Time{}) != "—" || dateTime(time.Time{}) != "—" || date(when) != "05/10/2026" || dateTime(when) != "05/10/2026 19:22" {
		t.Errorf("datas: %s %s", date(when), dateTime(when))
	}
}
