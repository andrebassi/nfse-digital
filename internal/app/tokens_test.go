package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
)

// TestIssueAndVerifyToken: o token sai com prefixo e entropia, o banco guarda
// só o hash (nunca o valor), conferir registra o uso, e token errado, sem
// prefixo ou revogado é recusado com o mesmo erro.
func TestIssueAndVerifyToken(t *testing.T) {
	ctx := context.Background()
	repo := memory.New(memory.DemoIssuer())
	service := &Service{Repo: repo, Now: fixedNow, Location: brt}
	plain, token, err := service.IssueToken(ctx)
	if err != nil || !strings.HasPrefix(plain, "nf_") || len(plain) < 40 || token.Hint != plain[len(plain)-4:] {
		t.Fatalf("token: %q %+v %v", plain, token, err)
	}
	if _, err := repo.APITokenByHash(ctx, plain); err == nil {
		t.Fatal("o valor do token não pode servir de chave no banco")
	}
	other, _, _ := service.IssueToken(ctx)
	if other == plain {
		t.Fatal("dois tokens iguais")
	}
	verified, err := service.VerifyToken(ctx, plain)
	if err != nil || verified.ID != token.ID || verified.LastUsedAt.IsZero() {
		t.Fatalf("verificar: %+v %v", verified, err)
	}
	if listed, _ := repo.APITokens(ctx); listed[1].LastUsedAt.IsZero() {
		t.Fatal("uso não registrado na lista")
	}
	for _, bad := range []string{"", "sem-prefixo", "nf_errado", strings.ToUpper(plain)} {
		if _, err := service.VerifyToken(ctx, bad); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	_ = repo.DeleteAPIToken(ctx, token.ID)
	if _, err := service.VerifyToken(ctx, plain); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revogado: %v", err)
	}
}

// TestTokenFailures: sem aleatoriedade não gera; falha do banco aparece.
func TestTokenFailures(t *testing.T) {
	ctx := context.Background()
	original := randomRead
	randomRead = func([]byte) (int, error) { return 0, errors.New("sem entropia") }
	service := &Service{Repo: memory.New(memory.DemoIssuer()), Now: fixedNow}
	if _, _, err := service.IssueToken(ctx); err == nil {
		t.Fatal("sem aleatoriedade deveria falhar")
	}
	randomRead = original
	broken := &Service{Repo: tokenFailRepo{memory.New(memory.DemoIssuer())}, Now: fixedNow}
	if _, _, err := broken.IssueToken(ctx); err == nil {
		t.Fatal("falha ao gravar deveria aparecer")
	}
	if _, err := broken.VerifyToken(ctx, "nf_qualquer"); err == nil || errors.Is(err, ErrInvalidToken) {
		t.Fatalf("falha do banco não é token inválido: %v", err)
	}
}

// tokenFailRepo falha nas operações de token.
type tokenFailRepo struct{ *memory.Repository }

// CreateAPIToken falha sempre.
func (tokenFailRepo) CreateAPIToken(context.Context, string, *domain.APIToken) error {
	return errors.New("disco cheio")
}

// APITokenByHash falha sempre.
func (tokenFailRepo) APITokenByHash(context.Context, string) (domain.APIToken, error) {
	return domain.APIToken{}, errors.New("banco fora")
}
