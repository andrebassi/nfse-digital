package maildocs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nfse/internal/domain"
)

// reference é a NFS-e real anonimizada da pasta testdata/ da raiz.
func reference(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "nfse-reference.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestInvoiceDocuments: com XML guardado, o PDF sai dele e o XML vai igual;
// sem XML, busca na SEFIN; sem busca configurada, falha da busca e XML
// ilegível são erro com mensagem.
func TestInvoiceDocuments(t *testing.T) {
	ctx := context.Background()
	xml := reference(t)
	pdf, gotXML, err := Documents{}.Invoice(ctx, domain.Invoice{XML: xml})
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) || !bytes.Equal(gotXML, xml) {
		t.Fatalf("com XML: %v", err)
	}
	fetched := 0
	docs := Documents{FetchXML: func(context.Context, domain.Invoice) ([]byte, error) { fetched++; return xml, nil }}
	if _, gotXML, err := docs.Invoice(ctx, domain.Invoice{ID: 3}); err != nil || fetched != 1 || !bytes.Equal(gotXML, xml) {
		t.Fatalf("buscado: %v (%d)", err, fetched)
	}
	if _, _, err := (Documents{}).Invoice(ctx, domain.Invoice{ID: 3}); err == nil || !strings.Contains(err.Error(), "#3") {
		t.Fatalf("sem busca: %v", err)
	}
	failing := Documents{FetchXML: func(context.Context, domain.Invoice) ([]byte, error) { return nil, errors.New("SEFIN fora") }}
	if _, _, err := failing.Invoice(ctx, domain.Invoice{ID: 3}); err == nil || !strings.Contains(err.Error(), "SEFIN fora") {
		t.Fatalf("busca falhou: %v", err)
	}
	if _, _, err := (Documents{}).Invoice(ctx, domain.Invoice{XML: []byte("<lixo")}); err == nil {
		t.Fatal("XML ilegível devia falhar")
	}
}

// TestSampleDocuments: o exemplo é um PDF de verdade e um XML que diz, logo
// no começo, que é simulação; texto com & e < do cadastro sai escapado.
func TestSampleDocuments(t *testing.T) {
	issuer := domain.Issuer{CNPJ: "11222333000181", LegalName: "EMITENTE & CIA", CityCode: "3505708"}
	customer := domain.Customer{Name: "CLIENTE <LTDA>", Document: "56789012000100"}
	invoice := domain.SampleInvoice(issuer, customer, time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC))
	pdf, xml, err := Documents{}.Sample(context.Background(), issuer, customer, invoice)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("PDF de exemplo: %v", err)
	}
	text := string(xml)
	if !strings.Contains(text[:200], SimulationMark) || !strings.Contains(text, "EMITENTE &amp; CIA") || !strings.Contains(text, "CLIENTE &lt;LTDA&gt;") || !strings.Contains(text, "<vServ>1500.00</vServ>") {
		t.Fatalf("XML de exemplo:\n%s", text)
	}
}
