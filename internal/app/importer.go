package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// ErrOtherIssuer recusa XML de outro CNPJ quando o emitente já está configurado.
var ErrOtherIssuer = errors.New("o XML é de outro emitente; use a opção de sobrescrever o emitente se for isso mesmo")

// Import grava o que veio de um XML de NFS-e: emitente (se vazio ou se
// overwriteIssuer), tomador por documento, modelo por nome e a nota pela
// chave de acesso. O XML já foi lido e validado pelo chamador.
func (s *Service) Import(ctx context.Context, imported domain.ImportedNFSe, templateName string, overwriteIssuer bool) (domain.ImportReport, error) {
	report := domain.ImportReport{}

	current, err := s.Repo.Issuer(ctx)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		if err := s.Repo.SaveIssuer(ctx, imported.Issuer); err != nil {
			return report, err
		}
		report.Issuer = "criado a partir do XML"
	case err != nil:
		return report, err
	case current.CNPJ != imported.Issuer.CNPJ && !overwriteIssuer:
		return report, ErrOtherIssuer
	case overwriteIssuer:
		// Mantém ambiente e o maior próximo número: importar nota antiga não
		// pode fazer a numeração andar para trás.
		merged := imported.Issuer
		merged.Environment = current.Environment
		merged.NextDPSNumber = max(current.NextDPSNumber, imported.Issuer.NextDPSNumber)
		if err := s.Repo.SaveIssuer(ctx, merged); err != nil {
			return report, err
		}
		report.Issuer = "atualizado a partir do XML"
	default:
		report.Issuer = "mantido (já configurado)"
	}

	customer, created, err := s.upsertCustomer(ctx, imported.Customer)
	if err != nil {
		return report, err
	}
	report.Customer = map[bool]string{true: "criado", false: "atualizado"}[created] + ": " + customer.Name

	template := imported.Template
	template.Name = strings.TrimSpace(templateName)
	if template.Name == "" {
		template.Name = "Importado — " + customer.Name
	}
	template.CustomerID = customer.ID
	matched, err := s.matchTemplate(ctx, template, customer)
	if err != nil {
		return report, err
	}
	template.ID, template.Name = matched.ID, matched.Name
	report.Template = "criado: " + template.Name
	if template.ID != 0 {
		report.Template = "atualizado: " + template.Name
	}
	if err := s.Repo.SaveTemplate(ctx, &template); err != nil {
		return report, err
	}

	invoice := imported.Invoice
	if invoice.AccessKey == "" {
		report.Invoice = "sem nota (o arquivo é uma DPS, não uma NFS-e autorizada)"
		return report, nil
	}
	invoices, err := s.Repo.Invoices(ctx)
	if err != nil {
		return report, err
	}
	for _, existing := range invoices {
		if existing.AccessKey == invoice.AccessKey {
			report.Invoice = "já existia: nº " + invoice.NFSeNumber
			return report, nil
		}
	}
	invoice.CustomerID, invoice.TemplateID = customer.ID, template.ID
	// Nota importada não teve número de DPS reservado aqui: zero evita colidir
	// com a numeração própria no índice único (ambiente, série, número).
	invoice.DPSNumber = 0
	if err := s.Repo.SaveInvoice(ctx, &invoice); err != nil {
		return report, fmt.Errorf("gravar nota importada: %w", err)
	}
	report.Invoice = "importada: nº " + invoice.NFSeNumber
	return report, nil
}

// upsertCustomer acha o tomador pelo documento e completa os campos vazios,
// ou cria um novo. Devolve se criou.
func (s *Service) upsertCustomer(ctx context.Context, incoming domain.Customer) (domain.Customer, bool, error) {
	if err := incoming.Validate(); err != nil {
		return incoming, false, fmt.Errorf("tomador do XML inválido: %w", err)
	}
	found, err := s.Repo.Customers(ctx, incoming.Document)
	if err != nil {
		return incoming, false, err
	}
	for _, existing := range found {
		if existing.Document == incoming.Document {
			merged := existing
			fill := func(target *string, value string) {
				if *target == "" {
					*target = value
				}
			}
			fill(&merged.Email, incoming.Email)
			fill(&merged.CityCode, incoming.CityCode)
			fill(&merged.ZIP, incoming.ZIP)
			fill(&merged.Street, incoming.Street)
			fill(&merged.Number, incoming.Number)
			fill(&merged.Complement, incoming.Complement)
			fill(&merged.District, incoming.District)
			return merged, false, s.Repo.SaveCustomer(ctx, &merged)
		}
	}
	return incoming, true, s.Repo.SaveCustomer(ctx, &incoming)
}

// matchTemplate acha o modelo que a importação atualiza, para importar de
// novo nunca criar um segundo. Ordem: o do mesmo tomador com o mesmo nome;
// senão o do mesmo tomador e mesmo código de serviço (o dono pode ter
// renomeado — o nome dele fica). Sem nenhum, é modelo novo: se o nome já é
// de um modelo de OUTRO tomador (dois tomadores com a mesma razão social),
// o CPF/CNPJ entra no nome, em vez de sobrescrever o modelo do outro.
func (s *Service) matchTemplate(ctx context.Context, wanted domain.Template, customer domain.Customer) (domain.Template, error) {
	all, err := s.Repo.Templates(ctx, "")
	if err != nil {
		return domain.Template{}, err
	}
	for _, existing := range all {
		if existing.CustomerID == customer.ID && existing.Name == wanted.Name {
			return existing, nil
		}
	}
	for _, existing := range all {
		if existing.CustomerID == customer.ID && existing.NationalTaxCode == wanted.NationalTaxCode {
			return existing, nil
		}
	}
	created := domain.Template{Name: wanted.Name}
	for _, existing := range all {
		if existing.Name == wanted.Name {
			created.Name = wanted.Name + " (" + domain.FormatDocument(customer.Document) + ")"
			break
		}
	}
	return created, nil
}
