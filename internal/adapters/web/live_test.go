package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"nfse/internal/domain"
)

// TestDANFSeDownload: sem o gerador ligado o DANFSe não existe; ligado, o
// botão aparece só na nota autorizada, o PDF sai como anexo e nota sem chave
// responde 409.
func TestDANFSeDownload(t *testing.T) {
	f := newFixture(t, nil, nil)
	ctx := context.Background()
	authorized := domain.Invoice{Environment: domain.EnvHomologation, Status: domain.StatusAuthorized, AccessKey: "K1", CustomerID: 1, IssuedAt: f.server.now()}
	rejected := domain.Invoice{Environment: domain.EnvHomologation, Status: domain.StatusRejected, CustomerID: 1, IssuedAt: f.server.now()}
	_ = f.repo.SaveInvoice(ctx, &authorized)
	_ = f.repo.SaveInvoice(ctx, &rejected)
	detail := "/invoices?period=90&id=" + itoa(authorized.ID)

	if body := f.get(detail).Body.String(); strings.Contains(body, "Baixar DANFSe") {
		t.Fatal("sem gerador, o botão do DANFSe não deveria aparecer")
	}
	if response := f.get("/invoices/" + itoa(authorized.ID) + "/danfse"); response.Code != http.StatusNotFound {
		t.Fatalf("DANFSe sem gerador: %d", response.Code)
	}

	failNext := false
	f.server.UseDANFSe(func(_ context.Context, invoice domain.Invoice) ([]byte, string, error) {
		if failNext {
			return nil, "", errors.New("SEFIN fora")
		}
		return []byte("%PDF-1.3 teste"), "nfse-" + invoice.AccessKey + ".pdf", nil
	})
	body := f.get(detail).Body.String()
	if !strings.Contains(body, "/invoices/"+itoa(authorized.ID)+"/danfse") {
		t.Fatal("com gerador, o botão deveria aparecer")
	}
	if body := f.get("/invoices?period=90&id=" + itoa(rejected.ID)).Body.String(); strings.Contains(body, "Baixar DANFSe") {
		t.Fatal("nota rejeitada não tem DANFSe")
	}
	pdf := f.get("/invoices/" + itoa(authorized.ID) + "/danfse")
	if pdf.Code != http.StatusOK || pdf.Header().Get("Content-Type") != "application/pdf" || !strings.Contains(pdf.Header().Get("Content-Disposition"), `filename="nfse-K1.pdf"`) {
		t.Fatalf("PDF: %d %v", pdf.Code, pdf.Header())
	}
	if response := f.get("/invoices/" + itoa(rejected.ID) + "/danfse"); response.Code != http.StatusConflict {
		t.Fatalf("rejeitada: %d", response.Code)
	}
	if response := f.get("/invoices/999/danfse"); response.Code != http.StatusNotFound {
		t.Fatalf("inexistente: %d", response.Code)
	}
	failNext = true
	if response := f.get("/invoices/" + itoa(authorized.ID) + "/danfse"); response.Code != http.StatusInternalServerError {
		t.Fatalf("falha ao gerar: %d", response.Code)
	}
}
