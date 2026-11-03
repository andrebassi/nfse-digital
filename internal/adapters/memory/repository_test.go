package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// brt é UTC-3 fixo, para o teste não depender do tzdata da máquina.
var brt = time.FixedZone("BRT", -3*60*60)

// TestSeedHasRealData trava o que o protótipo mostra: 5 tomadores, 5 modelos,
// 5 notas, a rejeitada com E0039 e a próxima DPS 1001.
func TestSeedHasRealData(t *testing.T) {
	ctx := context.Background()
	repo := NewSeeded(brt)
	customers, _ := repo.Customers(ctx, "")
	templates, _ := repo.Templates(ctx, "")
	invoices, _ := repo.Invoices(ctx)
	if len(customers) != 5 || len(templates) != 5 || len(invoices) != 5 {
		t.Fatalf("seed: %d tomadores, %d modelos, %d notas", len(customers), len(templates), len(invoices))
	}
	if invoices[0].Status != domain.StatusRejected || invoices[0].ErrorCode != "E0039" {
		t.Fatalf("a nota mais recente deveria ser a rejeitada: %+v", invoices[0])
	}
	issuer, _ := repo.Issuer(ctx)
	if issuer.NextDPSNumber != 1001 || issuer.Validate() != nil {
		t.Fatalf("emitente: %+v", issuer)
	}
	for _, customer := range customers {
		if err := customer.Validate(); err != nil {
			t.Errorf("tomador do seed inválido %s: %v", customer.Name, err)
		}
	}
	for _, template := range templates {
		if err := template.Validate(); err != nil {
			t.Errorf("modelo do seed inválido %s: %v", template.Name, err)
		}
	}
}

// TestSearchIgnoresAccentAndCase acha por nome sem acento, por CNPJ com e sem
// máscara e pelo nome do tomador do modelo.
func TestSearchIgnoresAccentAndCase(t *testing.T) {
	ctx := context.Background()
	repo := NewSeeded(brt)
	cases := map[string]int{"ipe": 1, "IPE": 1, "56.789.012": 1, "56789012": 1, "zzz": 0, "  ": 5}
	for query, want := range cases {
		got, _ := repo.Customers(ctx, query)
		if len(got) != want {
			t.Errorf("Customers(%q) = %d; want %d", query, len(got), want)
		}
	}
	byCustomer, _ := repo.Templates(ctx, "cedro")
	byService, _ := repo.Templates(ctx, "suporte técnico")
	if len(byCustomer) != 1 || len(byService) != 3 {
		t.Errorf("modelos: por tomador %d, por serviço %d", len(byCustomer), len(byService))
	}
}

// TestCrudAndNotFound cobre criar, atualizar, buscar e o erro de ID inexistente
// em todas as entidades.
func TestCrudAndNotFound(t *testing.T) {
	ctx := context.Background()
	repo := New(DemoIssuer())

	customer := domain.Customer{Name: "A", Document: "56789012000100"}
	if err := repo.SaveCustomer(ctx, &customer); err != nil || customer.ID == 0 {
		t.Fatal("criar tomador")
	}
	customer.Name = "B"
	_ = repo.SaveCustomer(ctx, &customer)
	if got, _ := repo.Customer(ctx, customer.ID); got.Name != "B" {
		t.Fatal("atualizar tomador")
	}

	template := domain.Template{Name: "M"}
	_ = repo.SaveTemplate(ctx, &template)
	if got, err := repo.Template(ctx, template.ID); err != nil || got.Name != "M" {
		t.Fatal("buscar modelo")
	}

	invoice := domain.Invoice{Amount: 1}
	_ = repo.SaveInvoice(ctx, &invoice)
	invoice.Amount = 2
	_ = repo.SaveInvoice(ctx, &invoice)
	if got, _ := repo.Invoice(ctx, invoice.ID); got.Amount != 2 {
		t.Fatal("atualizar nota")
	}

	first, second := domain.Schedule{Day: 28}, domain.Schedule{Day: 5}
	_ = repo.SaveSchedule(ctx, &first)
	_ = repo.SaveSchedule(ctx, &second)
	first.Day = 27
	_ = repo.SaveSchedule(ctx, &first)
	if list, _ := repo.Schedules(ctx); len(list) != 2 || list[0].Day != 5 || list[1].Day != 27 {
		t.Fatalf("agendamentos: %+v", list)
	}

	issuer := DemoIssuer()
	issuer.LegalName = "OUTRA"
	_ = repo.SaveIssuer(ctx, issuer)
	if got, _ := repo.Issuer(ctx); got.LegalName != "OUTRA" {
		t.Fatal("salvar emitente")
	}

	missing := int64(9999)
	checks := []error{
		repo.SaveCustomer(ctx, &domain.Customer{ID: missing}),
		repo.SaveTemplate(ctx, &domain.Template{ID: missing}),
		repo.SaveInvoice(ctx, &domain.Invoice{ID: missing}),
		repo.SaveSchedule(ctx, &domain.Schedule{ID: missing}),
	}
	_, err1 := repo.Customer(ctx, missing)
	_, err2 := repo.Template(ctx, missing)
	_, err3 := repo.Invoice(ctx, missing)
	checks = append(checks, err1, err2, err3)
	for i, err := range checks {
		if !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("verificação %d: %v", i, err)
		}
	}
}

// TestReserveDPSNumberIsUniqueUnderConcurrency: 50 emissões em paralelo nunca
// recebem o mesmo número, e o contador avança 50.
func TestReserveDPSNumberIsUniqueUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	repo := New(DemoIssuer())
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[int64]bool{}
	for range 50 {
		wg.Go(func() {
			series, number, _ := repo.ReserveDPSNumber(ctx)
			mu.Lock()
			defer mu.Unlock()
			if series != "1" || seen[number] {
				t.Errorf("número repetido ou série errada: %s/%d", series, number)
			}
			seen[number] = true
		})
	}
	wg.Wait()
	if issuer, _ := repo.Issuer(ctx); issuer.NextDPSNumber != 1051 {
		t.Fatalf("contador = %d", issuer.NextDPSNumber)
	}
}
