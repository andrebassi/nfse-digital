package sqlite

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// TestEmailPersistence: remetente, envio do tomador e registro de envios
// voltam como foram gravados; regravar substitui; cada conta só enxerga o
// dela; antes de gravar, as leituras são ErrNotFound (ou lista vazia).
func TestEmailPersistence(t *testing.T) {
	repo, _ := openTemp(t)
	base := context.Background()
	alice := domain.Account{Name: "Alice", Email: "alice@exemplo.example", PasswordHash: []byte("h")}
	bruno := domain.Account{Name: "Bruno", Email: "bruno@exemplo.example", PasswordHash: []byte("h")}
	if err := errors.Join(repo.CreateAccount(base, &alice), repo.CreateAccount(base, &bruno)); err != nil {
		t.Fatal(err)
	}
	ctxA, ctxB := ports.WithAccount(base, alice.ID), ports.WithAccount(base, bruno.ID)

	if _, err := repo.EmailSender(ctxA); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("remetente antes de gravar: %v", err)
	}
	if _, err := repo.CustomerEmail(ctxA, 7); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("envio antes de gravar: %v", err)
	}
	verified := time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)
	sender := domain.EmailSender{Address: "alice@gmail.example", Name: "Financeiro", SealedPassword: []byte{1, 2, 3}, VerifiedAt: verified}
	if err := repo.SaveEmailSender(ctxA, sender); err != nil {
		t.Fatal(err)
	}
	sender.Name = "Outro"
	_ = repo.SaveEmailSender(ctxA, sender)
	got, err := repo.EmailSender(ctxA)
	if err != nil || got.Address != sender.Address || got.Name != "Outro" || !bytes.Equal(got.SealedPassword, []byte{1, 2, 3}) || !got.VerifiedAt.Equal(verified) {
		t.Fatalf("remetente: %+v %v", got, err)
	}
	if _, err := repo.EmailSender(ctxB); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("o Bruno enxergou o remetente da Alice")
	}

	settings := domain.CustomerEmail{CustomerID: 7, AutoSend: true, To: "a@b.example", CC: "c@d.example", Subject: "S", Body: "B", AttachPDF: true}
	_ = repo.SaveCustomerEmail(ctxA, settings)
	settings.Subject = "S2"
	if err := repo.SaveCustomerEmail(ctxA, settings); err != nil {
		t.Fatal(err)
	}
	if saved, err := repo.CustomerEmail(ctxA, 7); err != nil || saved != settings {
		t.Fatalf("envio: %+v %v", saved, err)
	}
	if _, err := repo.CustomerEmail(ctxB, 7); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal("o Bruno enxergou o envio da Alice")
	}

	first := domain.InvoiceEmail{InvoiceID: 5, SentAt: verified, To: "a@b.example", Automatic: true}
	second := domain.InvoiceEmail{InvoiceID: 5, SentAt: verified.Add(time.Hour), To: "a@b.example", Error: "Gmail fora"}
	other := domain.InvoiceEmail{InvoiceID: 6, SentAt: verified, To: "x@y.example"}
	for _, sent := range []*domain.InvoiceEmail{&first, &second, &other} {
		if err := repo.RecordInvoiceEmail(ctxA, sent); err != nil || sent.ID == 0 {
			t.Fatalf("registrar: %v", err)
		}
	}
	list, err := repo.InvoiceEmails(ctxA, 5)
	if err != nil || len(list) != 2 || list[0].ID != second.ID || list[0].Error != "Gmail fora" || !list[1].Automatic || !list[1].SentAt.Equal(verified) {
		t.Fatalf("histórico: %+v %v", list, err)
	}
	if list, _ := repo.InvoiceEmails(ctxB, 5); len(list) != 0 {
		t.Fatal("o Bruno enxergou os envios da Alice")
	}
	repo.Close()
	if _, err := repo.InvoiceEmails(ctxA, 5); err == nil {
		t.Fatal("banco fechado devia falhar")
	}
}
