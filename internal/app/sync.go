package app

import (
	"context"
	"errors"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// SyncReport conta o que a sincronização fez.
type SyncReport struct {
	Checked    int
	Authorized int
	Cancelled  int
	Unchanged  int
	Failed     int
}

// Sync alinha o estado local com a SEFIN, só lendo de lá, para as notas do
// ambiente informado:
//   - pendente sem chave: procura a chave pela DPS; achou, vira autorizada
//     com o número da NFS-e; não achou (E2404), continua pendente;
//   - autorizada com chave: procura evento de cancelamento feito fora daqui.
//
// Nunca reenvia nada: reenviar é decisão do dono, depois de saber o estado.
func (s *Service) Sync(ctx context.Context, consulter ports.Consulter, parsers PortalParsers, environment domain.Environment) (SyncReport, error) {
	report := SyncReport{}
	issuer, err := s.Repo.Issuer(ctx)
	if err != nil {
		return report, err
	}
	invoices, err := s.Repo.Invoices(ctx)
	if err != nil {
		return report, err
	}
	for _, invoice := range invoices {
		if invoice.Environment != environment || invoice.Simulated {
			continue
		}
		before := invoice.Status
		switch {
		case invoice.Status == domain.StatusPending && invoice.AccessKey == "" && invoice.DPSNumber > 0:
			report.Checked++
			id := domain.DPSID(issuer.CityCode, domain.KindCNPJ, issuer.CNPJ, invoice.Series, invoice.DPSNumber)
			key, err := consulter.AccessKeyByDPS(ctx, id)
			if errors.Is(err, ports.ErrNotFound) {
				report.Unchanged++
				continue
			}
			if err != nil {
				report.Failed++
				continue
			}
			invoice.AccessKey, invoice.Status, invoice.ErrorMessage = key, domain.StatusAuthorized, ""
			s.applyDocuments(ctx, consulter, parsers, &invoice)
		case invoice.Status == domain.StatusAuthorized && invoice.AccessKey != "":
			report.Checked++
			s.applyDocuments(ctx, consulter, parsers, &invoice)
		default:
			continue
		}
		if invoice.Status == before && before != domain.StatusPending {
			report.Unchanged++
			continue
		}
		if err := s.Repo.SaveInvoice(ctx, &invoice); err != nil {
			return report, err
		}
		switch invoice.Status {
		case domain.StatusAuthorized:
			report.Authorized++
		case domain.StatusCancelled:
			report.Cancelled++
		}
	}
	return report, nil
}

// applyDocuments lê a NFS-e e os eventos da nota: preenche o número e marca
// cancelada quando há evento de cancelamento. Falha de leitura não muda nada.
func (s *Service) applyDocuments(ctx context.Context, consulter ports.Consulter, parsers PortalParsers, invoice *domain.Invoice) {
	batch, err := consulter.Documents(ctx, invoice.AccessKey)
	if err != nil {
		return
	}
	for _, document := range batch.Documents {
		switch document.Type {
		case "NFSE":
			if note, err := parsers.NFSe(document.XML); err == nil {
				if invoice.NFSeNumber == "" {
					invoice.NFSeNumber = note.Invoice.NFSeNumber
				}
				if len(invoice.XML) == 0 {
					invoice.XML = document.XML
				}
			}
		case "EVENTO":
			if key, cancels, err := parsers.Event(document.XML); err == nil && cancels && key == invoice.AccessKey {
				invoice.Status = domain.StatusCancelled
			}
		}
	}
}
