package memory

import (
	"context"
	"slices"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// EmailSender devolve o remetente; ErrNotFound antes do primeiro cadastro.
func (r *Repository) EmailSender(context.Context) (domain.EmailSender, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.sender == nil {
		return domain.EmailSender{}, ports.ErrNotFound
	}
	sender := *r.sender
	sender.SealedPassword = slices.Clone(sender.SealedPassword)
	return sender, nil
}

// SaveEmailSender grava (ou substitui) o remetente.
func (r *Repository) SaveEmailSender(_ context.Context, sender domain.EmailSender) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sender.SealedPassword = slices.Clone(sender.SealedPassword)
	r.sender = &sender
	return nil
}

// CustomerEmail devolve o envio salvo do tomador; ErrNotFound se nunca salvo.
func (r *Repository) CustomerEmail(_ context.Context, customerID int64) (domain.CustomerEmail, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	settings, ok := r.customerEmails[customerID]
	if !ok {
		return domain.CustomerEmail{CustomerID: customerID}, ports.ErrNotFound
	}
	return settings, nil
}

// SaveCustomerEmail grava (ou substitui) o envio do tomador.
func (r *Repository) SaveCustomerEmail(_ context.Context, settings domain.CustomerEmail) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.customerEmails[settings.CustomerID] = settings
	return nil
}

// RecordInvoiceEmail grava um envio e preenche o ID.
func (r *Repository) RecordInvoiceEmail(_ context.Context, sent *domain.InvoiceEmail) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sent.ID = r.nextID()
	r.invoiceEmails = append(r.invoiceEmails, *sent)
	return nil
}

// InvoiceEmails lista os envios da nota, do mais recente para o mais antigo.
func (r *Repository) InvoiceEmails(_ context.Context, invoiceID int64) ([]domain.InvoiceEmail, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []domain.InvoiceEmail
	for i := len(r.invoiceEmails) - 1; i >= 0; i-- {
		if r.invoiceEmails[i].InvoiceID == invoiceID {
			list = append(list, r.invoiceEmails[i])
		}
	}
	return list, nil
}
