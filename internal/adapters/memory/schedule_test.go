package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestScheduleRuns: mesma regra do SQLite — segunda nota do mesmo
// agendamento e mês é ErrConflict, regravar a própria nota não é; aviso marca
// uma vez por mês; apagar duas vezes é ErrNotFound.
func TestScheduleRuns(t *testing.T) {
	ctx := context.Background()
	repo := New(DemoIssuer())
	schedule := domain.Schedule{Day: 28, Active: true}
	_ = repo.SaveSchedule(ctx, &schedule)
	first := domain.Invoice{ScheduleID: schedule.ID, SchedulePeriod: "2026-11"}
	if err := repo.SaveInvoice(ctx, &first); err != nil {
		t.Fatal(err)
	}
	first.Status = domain.StatusAuthorized
	if err := repo.SaveInvoice(ctx, &first); err != nil {
		t.Fatalf("regravar a própria nota: %v", err)
	}
	second := domain.Invoice{ScheduleID: schedule.ID, SchedulePeriod: "2026-11"}
	if err := repo.SaveInvoice(ctx, &second); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("segunda do mês: %v", err)
	}
	keyed := domain.Invoice{IdempotencyKey: "pedido-1"}
	_ = repo.SaveInvoice(ctx, &keyed)
	if err := repo.SaveInvoice(ctx, &domain.Invoice{IdempotencyKey: "pedido-1"}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("mesma chave: %v", err)
	}
	for i, want := range []bool{true, false} {
		if claimed, _ := repo.ClaimNotification(ctx, schedule.ID, "2026-11"); claimed != want {
			t.Fatalf("aviso %d: %v", i, claimed)
		}
	}
	if claimed, _ := repo.ClaimNotification(ctx, 999, "2026-11"); claimed {
		t.Fatal("agendamento inexistente marcou aviso")
	}
	if err := repo.DeleteSchedule(ctx, schedule.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteSchedule(ctx, schedule.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("apagar de novo: %v", err)
	}
}

// TestAgentContacts: substitui o contato do mesmo cliente e ordena do mais recente.
func TestAgentContacts(t *testing.T) {
	ctx := context.Background()
	repo := New(DemoIssuer())
	base := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	_ = repo.RecordAgentContact(ctx, domain.AgentContact{Client: "a", Tool: "t1", At: base})
	_ = repo.RecordAgentContact(ctx, domain.AgentContact{Client: "b", Tool: "t2", At: base.Add(time.Minute)})
	_ = repo.RecordAgentContact(ctx, domain.AgentContact{Client: "a", Tool: "t3", At: base.Add(2 * time.Minute)})
	contacts, _ := repo.AgentContacts(ctx)
	if len(contacts) != 2 || contacts[0].Client != "a" || contacts[0].Tool != "t3" || contacts[1].Client != "b" {
		t.Fatalf("contatos: %+v", contacts)
	}
}

// TestAPITokensAndCertificateStatus: mesmas regras do SQLite.
func TestAPITokensAndCertificateStatus(t *testing.T) {
	ctx := context.Background()
	repo := New(DemoIssuer())
	first := domain.APIToken{Hint: "abcd"}
	_ = repo.CreateAPIToken(ctx, "hash-1", &first)
	second := domain.APIToken{Hint: "wxyz"}
	_ = repo.CreateAPIToken(ctx, "hash-2", &second)
	if err := repo.CreateAPIToken(ctx, "hash-1", &domain.APIToken{}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("hash repetido: %v", err)
	}
	at := time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)
	if err := repo.TouchAPIToken(ctx, first.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := repo.TouchAPIToken(ctx, 999, at); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("tocar inexistente: %v", err)
	}
	if found, err := repo.APITokenByHash(ctx, "hash-1"); err != nil || !found.LastUsedAt.Equal(at) {
		t.Fatalf("por hash: %+v %v", found, err)
	}
	if _, err := repo.APITokenByHash(ctx, "x"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}
	if list, _ := repo.APITokens(ctx); len(list) != 2 || list[0].ID != second.ID {
		t.Fatalf("lista: %+v", list)
	}
	_ = repo.DeleteAPIToken(ctx, first.ID)
	if err := repo.DeleteAPIToken(ctx, first.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("apagar de novo: %v", err)
	}
	status := domain.CertificateStatus{Info: domain.CertificateInfo{CNPJ: "11222333000181"}}
	_ = repo.SaveCertificateStatus(ctx, status)
	if got, _ := repo.CertificateStatus(ctx); got.Info.CNPJ != "11222333000181" {
		t.Fatalf("certificado: %+v", got)
	}
}

// TestPasskeys: mesmas regras do SQLite.
func TestPasskeys(t *testing.T) {
	ctx := context.Background()
	repo := New(DemoIssuer())
	first := domain.Passkey{CredentialID: []byte("cred-1")}
	_ = repo.SavePasskey(ctx, &first)
	if err := repo.SavePasskey(ctx, &domain.Passkey{CredentialID: []byte("cred-1")}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("repetida: %v", err)
	}
	first.Data = []byte("novo")
	if err := repo.SavePasskey(ctx, &first); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePasskey(ctx, &domain.Passkey{ID: 999}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("inexistente: %v", err)
	}
	if list, _ := repo.Passkeys(ctx); len(list) != 1 || string(list[0].Data) != "novo" {
		t.Fatalf("lista: %+v", list)
	}
	_ = repo.DeletePasskeys(ctx)
	if list, _ := repo.Passkeys(ctx); len(list) != 0 {
		t.Fatal("não apagou")
	}
}
