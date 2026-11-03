package memory

import (
	"context"
	"errors"
	"testing"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestIssuerNotConfigured: emitente vazio se comporta como banco novo.
func TestIssuerNotConfigured(t *testing.T) {
	ctx := context.Background()
	repo := New(domain.Issuer{})
	if _, err := repo.Issuer(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("emitente: %v", err)
	}
	if _, _, err := repo.ReserveDPSNumber(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("reserva: %v", err)
	}
}

// TestAccounts: sem cadastro devolve ErrNotFound; várias contas, e-mail
// repetido recusado, busca por e-mail e pela conta do context.
func TestAccounts(t *testing.T) {
	ctx := context.Background()
	repo := New(DemoIssuer())
	if _, err := repo.Account(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("sem conta: %v", err)
	}
	first := domain.Account{Email: "dona@exemplo.example"}
	second := domain.Account{Email: "outra@exemplo.example"}
	_ = repo.CreateAccount(ctx, &first)
	_ = repo.CreateAccount(ctx, &second)
	if err := repo.CreateAccount(ctx, &domain.Account{Email: "dona@exemplo.example"}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("e-mail repetido: %v", err)
	}
	if account, err := repo.Account(ctx); err != nil || account.ID != first.ID {
		t.Fatalf("sem conta no context, a primeira: %+v, %v", account, err)
	}
	if account, _ := repo.Account(ports.WithAccount(ctx, second.ID)); account.Email != "outra@exemplo.example" {
		t.Fatalf("conta do context: %+v", account)
	}
	if _, err := repo.Account(ports.WithAccount(ctx, 999)); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("conta inexistente: %v", err)
	}
	if account, _ := repo.AccountByEmail(ctx, "outra@exemplo.example"); account.ID != second.ID {
		t.Fatalf("por e-mail: %+v", account)
	}
	if _, err := repo.AccountByEmail(ctx, "x@y.z"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("e-mail inexistente: %v", err)
	}
	if list, _ := repo.Accounts(ctx); len(list) != 2 {
		t.Fatalf("lista: %+v", list)
	}
}
