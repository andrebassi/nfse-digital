package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestEmailInMemory: remetente e envio voltam como gravados (cópias: mexer
// no que voltou não muda o guardado); o histórico vem do mais recente para o
// mais antigo, só da nota pedida; excluir a conta limpa tudo.
func TestEmailInMemory(t *testing.T) {
	repo := New(DemoIssuer())
	ctx := context.Background()
	_ = repo.CreateAccount(ctx, &domain.Account{Email: "dona@exemplo.example"})
	if _, err := repo.EmailSender(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("remetente vazio: %v", err)
	}
	if _, err := repo.CustomerEmail(ctx, 3); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("envio vazio: %v", err)
	}
	_ = repo.SaveEmailSender(ctx, domain.EmailSender{Address: "a@b.example", SealedPassword: []byte{1}})
	got, _ := repo.EmailSender(ctx)
	got.SealedPassword[0] = 9
	if again, _ := repo.EmailSender(ctx); again.SealedPassword[0] != 1 || again.Address != "a@b.example" {
		t.Fatalf("remetente alterado por fora: %+v", again)
	}
	settings := domain.CustomerEmail{CustomerID: 3, To: "x@y.example", AutoSend: true}
	_ = repo.SaveCustomerEmail(ctx, settings)
	if saved, err := repo.CustomerEmail(ctx, 3); err != nil || saved != settings {
		t.Fatalf("envio: %+v %v", saved, err)
	}
	now := time.Now()
	for _, sent := range []domain.InvoiceEmail{{InvoiceID: 5, SentAt: now}, {InvoiceID: 6}, {InvoiceID: 5, Error: "falhou"}} {
		_ = repo.RecordInvoiceEmail(ctx, &sent)
	}
	list, _ := repo.InvoiceEmails(ctx, 5)
	if len(list) != 2 || list[0].Error != "falhou" || list[1].ID == 0 {
		t.Fatalf("histórico: %+v", list)
	}
	if err := repo.DeleteAccount(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EmailSender(ctx); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("excluir a conta deixou o remetente")
	}
	if list, _ := repo.InvoiceEmails(ctx, 5); len(list) != 0 {
		t.Fatal("excluir a conta deixou o histórico")
	}
}
