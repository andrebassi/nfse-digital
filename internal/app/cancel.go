package app

import (
	"context"
	"strings"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// deadlineHint orienta quando a SEFIN recusa por prazo: passado o prazo do
// município, o cancelamento só sai pela prefeitura (análise fiscal).
const deadlineHint = " — fora do prazo de cancelamento pelo emissor nacional: peça o cancelamento na prefeitura do município de incidência."

// CancelReport diz o que aconteceu com o pedido de cancelamento.
type CancelReport struct {
	Invoice    domain.Invoice
	Registered bool
}

// Cancel monta o evento, envia pelo canceller e grava o resultado. Aceito, a
// nota vira cancelled; recusado, ela segue authorized com o erro da SEFIN
// guardado, para o painel mostrar por que não cancelou.
func (s *Service) Cancel(ctx context.Context, canceller ports.Canceller, invoiceID int64, reason, description, appVersion string, confirmProduction bool) (CancelReport, error) {
	invoice, err := s.Repo.Invoice(ctx, invoiceID)
	if err != nil {
		return CancelReport{}, err
	}
	// Cancelar nota de produção é tão irreversível quanto emitir.
	if err := EnsureEnvironment(invoice.Environment, confirmProduction); err != nil {
		return CancelReport{Invoice: invoice}, err
	}
	issuer, err := s.Repo.Issuer(ctx)
	if err != nil {
		return CancelReport{}, err
	}
	event, err := domain.BuildCancelEvent(issuer, invoice, reason, description, appVersion, s.Now().In(s.Location))
	if err != nil {
		return CancelReport{Invoice: invoice}, err
	}
	result, err := canceller.Cancel(ctx, event)
	if err != nil {
		return CancelReport{Invoice: invoice}, err
	}
	if result.Registered {
		invoice.Status = domain.StatusCancelled
		invoice.ErrorCode, invoice.ErrorMessage = "", ""
	} else {
		invoice.ErrorCode = result.ErrorCode
		invoice.ErrorMessage = domain.TranslateSefinError(result.ErrorCode, result.ErrorMessage)
		if strings.Contains(strings.ToLower(result.ErrorMessage), "prazo") {
			invoice.ErrorMessage += deadlineHint
		}
	}
	if err := s.Repo.SaveInvoice(ctx, &invoice); err != nil {
		return CancelReport{Invoice: invoice}, err
	}
	return CancelReport{Invoice: invoice, Registered: result.Registered}, nil
}
