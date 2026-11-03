package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// fillAccount grava uma linha da conta em cada tabela que guarda dado por
// conta, pelos métodos do próprio repositório.
func fillAccount(t *testing.T, repo *Repository, ctx context.Context, email string) {
	t.Helper()
	now := time.Date(2026, 10, 30, 3, 0, 0, 0, brt)
	customer := domain.Customer{Name: "Tomador", Document: "11222333000181"}
	template := domain.Template{Name: "Modelo", NationalTaxCode: "010701", Description: "Serviço", DefaultAmount: 100}
	steps := []error{
		repo.SaveIssuer(ctx, domain.Issuer{CNPJ: "11222333000181", LegalName: "Emitente " + email, CityCode: "3505708", Environment: domain.EnvHomologation, Series: "1", NextDPSNumber: 1}),
		repo.SaveCustomer(ctx, &customer),
		repo.SaveTemplate(ctx, &template),
		repo.SaveInvoice(ctx, &domain.Invoice{Environment: domain.EnvProduction, Series: "1", CustomerID: customer.ID, AccessKey: "NFS" + email, Status: domain.StatusAuthorized}),
		repo.SaveSchedule(ctx, &domain.Schedule{TemplateID: template.ID, Day: 5, Active: true}),
		repo.SaveCertificateStatus(ctx, domain.CertificateStatus{Info: domain.CertificateInfo{CNPJ: "11222333000181"}, CheckedAt: now}),
		repo.SaveSealedCertificate(ctx, []byte("selado")),
		repo.CreateAPIToken(ctx, "hash-"+email, &domain.APIToken{Hint: "nf_x"}),
		repo.SavePasskey(ctx, &domain.Passkey{CredentialID: []byte("cred-" + email), Data: []byte("{}"), Label: "chave"}),
		repo.RecordAgentContact(ctx, domain.AgentContact{Client: "claude-code", Tool: "list_invoices", At: now}),
		repo.SaveEmailSender(ctx, domain.EmailSender{Address: "dono@gmail.example", SealedPassword: []byte("selada")}),
		repo.SaveCustomerEmail(ctx, domain.DefaultCustomerEmail(customer)),
		repo.RecordInvoiceEmail(ctx, &domain.InvoiceEmail{InvoiceID: 1, To: "a@b.example", SentAt: now}),
	}
	for index, err := range steps {
		if err != nil {
			t.Fatalf("passo %d de %s: %v", index, email, err)
		}
	}
}

// rowsOf conta, em cada tabela com account_id, as linhas da conta.
func rowsOf(t *testing.T, repo *Repository, accountID int64) map[string]int {
	t.Helper()
	tables, err := repo.accountTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, table := range tables {
		var n int
		if err := repo.db.QueryRow(`SELECT COUNT(*) FROM "`+table+`" WHERE account_id = ?`, accountID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		counts[table] = n
	}
	return counts
}

// TestDeleteAccount: excluir a conta apaga TODA linha dela, em toda tabela
// que guarda dado por conta (lista tirada do schema), e a própria conta; a
// outra conta fica intacta; excluir de novo é ErrNotFound.
func TestDeleteAccount(t *testing.T) {
	repo, _ := openTemp(t)
	base := context.Background()
	gone, kept := domain.Account{Name: "Sai", Email: "sai@exemplo.example"}, domain.Account{Name: "Fica", Email: "fica@exemplo.example"}
	for _, account := range []*domain.Account{&gone, &kept} {
		account.PasswordHash = []byte("hash")
		if err := repo.CreateAccount(base, account); err != nil || account.ID == 0 {
			t.Fatalf("criar conta: %v", err)
		}
	}
	goneCtx, keptCtx := ports.WithAccount(base, gone.ID), ports.WithAccount(base, kept.ID)
	fillAccount(t, repo, goneCtx, gone.Email)
	fillAccount(t, repo, keptCtx, kept.Email)

	before := rowsOf(t, repo, gone.ID)
	if len(before) < 10 {
		t.Fatalf("o schema deveria ter pelo menos 10 tabelas por conta: %v", before)
	}
	for table, n := range before {
		if n == 0 {
			t.Errorf("o teste não gravou nada em %s: ampliar fillAccount", table)
		}
	}
	keptBefore := rowsOf(t, repo, kept.ID)

	if err := repo.DeleteAccount(goneCtx); err != nil {
		t.Fatal(err)
	}
	for table, n := range rowsOf(t, repo, gone.ID) {
		if n != 0 {
			t.Errorf("sobrou %d linha(s) da conta excluída em %s", n, table)
		}
	}
	if _, err := repo.AccountByEmail(base, gone.Email); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("a conta continua existindo: %v", err)
	}
	for table, n := range rowsOf(t, repo, kept.ID) {
		if n != keptBefore[table] {
			t.Errorf("a exclusão mexeu na outra conta em %s: %d → %d", table, keptBefore[table], n)
		}
	}
	if err := repo.DeleteAccount(goneCtx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("excluir de novo: %v", err)
	}
	if err := repo.For(0).DeleteAccount(base); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("sem conta na operação: %v", err)
	}
}
