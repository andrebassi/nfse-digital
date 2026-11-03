package turso

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestInsertIDLive confere, no Turso real, que o ID que o driver devolve
// depois do INSERT é o ID gravado de fato. Se não for, toda operação que usa
// o ID recém-criado (a conta do cadastro, o tomador da importação) mira a
// linha errada. Cria e apaga duas contas descartáveis.
func TestInsertIDLive(t *testing.T) {
	url := os.Getenv("NFSE_TURSO_LIVE_URL")
	if url == "" {
		t.Skip("NFSE_TURSO_LIVE_URL não definida: teste contra o Turso real fica de fora")
	}
	repo, err := Open(url, os.Getenv("TURSO_AUTH_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	base := context.Background()
	// As duas contas existem ao mesmo tempo: a segunda nasce com uma conta
	// já no banco, que é o caso real (o painel do dono já tinha a dele).
	created := []domain.Account{}
	for range 2 {
		account := domain.Account{Name: "Teste de ID", Email: "id-" + rand.Text()[:10] + "@exemplo.example", PasswordHash: []byte("x"), CreatedAt: time.Now()}
		if err := repo.CreateAccount(base, &account); err != nil {
			t.Fatal(err)
		}
		created = append(created, account)
	}
	for _, account := range created {
		stored, err := repo.AccountByEmail(base, account.Email)
		t.Logf("driver devolveu id %d; gravado com id %d", account.ID, stored.ID)
		if err != nil || stored.ID != account.ID {
			t.Errorf("ID do INSERT errado: driver %d, banco %d (%v)", account.ID, stored.ID, err)
		}
	}
	for _, account := range created {
		stored, _ := repo.AccountByEmail(base, account.Email)
		if err := repo.DeleteAccount(ports.WithAccount(base, stored.ID)); err != nil {
			t.Errorf("limpar a conta de teste: %v", err)
		}
	}
}

// TestIdleConnectionLive reproduz a máquina do Fly acordando da suspensão:
// uma consulta, 15 s parado (acima do prazo em que o Turso descarta o stream)
// e outra consulta, que tem de funcionar. Sem o SetConnMaxIdleTime, a segunda
// dava "stream not found" (log do Fly, 30/10/2026 06:25:57).
func TestIdleConnectionLive(t *testing.T) {
	url := os.Getenv("NFSE_TURSO_LIVE_URL")
	if url == "" {
		t.Skip("NFSE_TURSO_LIVE_URL não definida: teste contra o Turso real fica de fora")
	}
	repo, err := Open(url, os.Getenv("TURSO_AUTH_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if _, err := repo.Accounts(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Second)
	if _, err := repo.Accounts(context.Background()); err != nil {
		t.Fatalf("consulta depois de 15 s parado: %v", err)
	}
}

// TestDeleteAccountLive roda contra o Turso de verdade, só com
// NFSE_TURSO_LIVE_URL e TURSO_AUTH_TOKEN definidos (fora do gate: precisa de
// rede e de credencial). Cria uma conta descartável, grava dado nela, exclui
// e confere que nada sobrou — prova que a exclusão (que descobre as tabelas
// por pragma_table_info) funciona no libSQL remoto, não só no SQLite local.
// Nenhuma outra conta é tocada.
func TestDeleteAccountLive(t *testing.T) {
	url := os.Getenv("NFSE_TURSO_LIVE_URL")
	if url == "" {
		t.Skip("NFSE_TURSO_LIVE_URL não definida: teste contra o Turso real fica de fora")
	}
	repo, err := Open(url, os.Getenv("TURSO_AUTH_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	base := context.Background()
	email := "exclusao-" + rand.Text()[:10] + "@exemplo.example"
	account := domain.Account{Name: "Teste de exclusão", Email: domain.NormalizeEmail(email), PasswordHash: []byte("x"), CreatedAt: time.Now()}
	if err := repo.CreateAccount(base, &account); err != nil {
		t.Fatal(err)
	}
	ctx := ports.WithAccount(base, account.ID)
	customer := domain.Customer{Name: "Tomador de teste", Document: "11222333000181"}
	if err := repo.SaveCustomer(ctx, &customer); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveSealedCertificate(ctx, []byte("selado")); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAccount(ctx); err != nil {
		t.Fatalf("excluir no Turso: %v", err)
	}
	if _, err := repo.AccountByEmail(base, account.Email); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("a conta continua no Turso: %v", err)
	}
	if customers, _ := repo.Customers(ctx, ""); len(customers) != 0 {
		t.Fatalf("sobraram %d tomadores no Turso", len(customers))
	}
	if _, err := repo.SealedCertificate(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("o certificado selado sobrou no Turso: %v", err)
	}
}
