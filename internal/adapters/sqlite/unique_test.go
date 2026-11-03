package sqlite

import (
	"context"
	"errors"
	"testing"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestImportKeysAreUnique (migração 012): na mesma conta, a mesma NFS-e
// (chave de acesso) e o mesmo tomador (CPF/CNPJ) existem uma vez só — criar
// ou editar para repetir é ErrConflict. Em OUTRA conta, os mesmos valores
// valem: são empresas diferentes. Nota sem chave e tomador sem documento
// (exterior) não entram no índice.
func TestImportKeysAreUnique(t *testing.T) {
	repo, _ := openTemp(t)
	one, two := ports.WithAccount(context.Background(), 1), ports.WithAccount(context.Background(), 2)

	first := domain.Customer{Name: "Exemplo", Document: "11222333000181"}
	if err := repo.SaveCustomer(one, &first); err != nil {
		t.Fatal(err)
	}
	copied := domain.Customer{Name: "Exemplo de novo", Document: "11222333000181"}
	if err := repo.SaveCustomer(one, &copied); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("tomador repetido: %v", err)
	}
	other := domain.Customer{Name: "Outro", Document: "56789012000100"}
	_ = repo.SaveCustomer(one, &other)
	other.Document = "11222333000181"
	if err := repo.SaveCustomer(one, &other); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("editar para um CNPJ que já existe: %v", err)
	}
	elsewhere := domain.Customer{Name: "Exemplo", Document: "11222333000181"}
	if err := repo.SaveCustomer(two, &elsewhere); err != nil {
		t.Fatalf("o mesmo CNPJ em outra conta: %v", err)
	}
	for range 2 {
		foreign := domain.Customer{Name: "Foreign Inc", Foreign: true, Country: "US"}
		if err := repo.SaveCustomer(one, &foreign); err != nil {
			t.Fatalf("tomador sem documento: %v", err)
		}
	}

	note := func(key string) *domain.Invoice {
		return &domain.Invoice{Environment: domain.EnvProduction, Series: "1", CustomerID: first.ID, AccessKey: key, Status: domain.StatusAuthorized}
	}
	if err := repo.SaveInvoice(one, note("NFS35")); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveInvoice(one, note("NFS35")); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("nota repetida: %v", err)
	}
	if err := repo.SaveInvoice(two, note("NFS35")); err != nil {
		t.Fatalf("a mesma chave em outra conta: %v", err)
	}
	for range 2 {
		if err := repo.SaveInvoice(one, note("")); err != nil {
			t.Fatalf("nota sem chave de acesso: %v", err)
		}
	}
}
