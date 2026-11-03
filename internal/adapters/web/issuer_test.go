package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
)

// TestFirstUseWithoutIssuer: banco sem emitente mostra o aviso na barra
// lateral e, ao emitir, pede a configuração em vez de dar erro genérico.
func TestFirstUseWithoutIssuer(t *testing.T) {
	ctx := context.Background()
	repo := memory.New(domain.Issuer{})
	customer := domain.Customer{Name: "IPÊ INFORMÁTICA LTDA", Document: "56789012000100"}
	_ = repo.SaveCustomer(ctx, &customer)
	template := domain.Template{Name: "Ipê mensal", Description: "x", NationalTaxCode: "010701", ServiceCityCode: "3505708"}
	_ = repo.SaveTemplate(ctx, &template)
	f := newFixture(t, nil, repo)
	_ = repo.CreateAccount(ctx, &domain.Account{Name: "Dona", Email: "dona@exemplo.example"})

	if body := f.get("/invoices").Body.String(); !strings.Contains(body, "Emitente ainda não configurado") {
		t.Error("barra lateral sem o aviso de emitente")
	}
	// Defeito achado pelo e2e em 29/10/2026: Configurações dava 404 com o banco
	// vazio, justamente a tela onde o emitente é configurado.
	settings := f.get("/settings")
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), `name="next_dps_number" value="1"`) {
		t.Fatalf("configurações sem emitente: %d", settings.Code)
	}
	response := f.post("/emit", url.Values{"action": {"emit"}, "customer_id": {itoa(customer.ID)}, "template_id": {itoa(template.ID)}, "amount": {"10,00"}, "competence": {"2026-10-29"}, "description": {"x"}})
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "configure o emitente") {
		t.Fatalf("emitir sem emitente: %d", response.Code)
	}
}
