package app

import (
	"context"
	"errors"
	"testing"

	"nfse/internal/adapters/memory"
	"nfse/internal/adapters/simulated"
	"nfse/internal/domain"
)

// TestEmitWithoutIssuerAsksToConfigure: banco novo, sem emitente, devolve erro
// de campo pedindo a configuração, sem reservar número nem criar nota.
func TestEmitWithoutIssuerAsksToConfigure(t *testing.T) {
	ctx := context.Background()
	repo := memory.New(domain.Issuer{})
	customer := domain.Customer{Name: "IPÊ", Document: "56789012000100"}
	_ = repo.SaveCustomer(ctx, &customer)
	template := domain.Template{Name: "Ipê mensal", Description: "x", NationalTaxCode: "010701", ServiceCityCode: "3505708"}
	_ = repo.SaveTemplate(ctx, &template)
	service := &Service{Repo: repo, Emitter: simulated.Emitter{}, Now: fixedNow, Location: brt}

	_, err := service.Emit(ctx, EmitInput{CustomerID: customer.ID, TemplateID: template.ID, Amount: "10,00", Competence: "2026-10-29", Description: "x"})
	var fields domain.FieldErrors
	if !errors.As(err, &fields) || fields["issuer"] == "" {
		t.Fatalf("erro: %v", err)
	}
	if invoices, _ := repo.Invoices(ctx); len(invoices) != 0 {
		t.Fatal("criou nota sem emitente")
	}
}
