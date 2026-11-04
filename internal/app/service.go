// Package app tem os casos de uso: orquestra domínio e ports, sem saber se o
// pedido veio do painel, da CLI ou do MCP.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// Service reúne os casos de uso do emissor.
type Service struct {
	Repo     ports.Repository
	Emitter  ports.Emitter
	Now      func() time.Time
	Location *time.Location

	// SignUpEmails, quando não é nil, é a lista dos e-mails (normalizados)
	// que podem criar conta: o painel público não abre cadastro a qualquer um.
	// nil = cadastro aberto, como na máquina do dono.
	SignUpEmails map[string]bool

	// guard conta as senhas erradas seguidas, por cliente.
	guard loginGuard

	// Mailer, Sealer e Documents ligam o envio da nota por e-mail; com
	// qualquer um nil, o envio fica desligado (CLI, testes antigos).
	Mailer    ports.Mailer
	Sealer    ports.SecretSealer
	Documents ports.InvoiceDocuments

	// importLocks põe as importações da mesma conta em fila (um *sync.Mutex
	// por conta): duas rodando juntas passariam ao mesmo tempo pelo "já
	// existe?" e gravariam em dobro.
	importLocks sync.Map
}

// EmitInput é o que a tela de emissão envia, ainda como texto digitado.
type EmitInput struct {
	CustomerID  int64
	TemplateID  int64
	Amount      string
	Competence  string
	Description string
	// ScheduleID e SchedulePeriod vêm preenchidos só quando quem emite é o
	// agendador; o banco recusa a segunda nota do mesmo par.
	ScheduleID     int64
	SchedulePeriod string
	// ConfirmProduction é a confirmação explícita do canal para emitir com
	// valor fiscal (story 4.1); sem ela, emitente em produção é recusado.
	ConfirmProduction bool
	// IdempotencyKey, quando vem, faz o pedido repetido devolver a nota já
	// criada em vez de emitir outra (MCP, story 4.2).
	IdempotencyKey string
}

// Emit valida o pedido, reserva o número da DPS, chama o emissor e grava o
// resultado. Erros de digitação voltam como domain.FieldErrors sem gastar número.
// Resolved são as peças de uma emissão já validadas: o que Emit usa para
// montar a nota e o que Preview mostra ao dono antes de existir nota alguma.
type Resolved struct {
	Issuer     domain.Issuer
	Customer   domain.Customer
	Template   domain.Template
	Amount     domain.Money
	Competence time.Time
}

// resolve lê e valida tudo o que a emissão precisa, sem reservar número, sem
// gravar e sem transmitir. Emit e Preview compartilham esta função de
// propósito: espelho que não passa pelo mesmo caminho do envio pode divergir
// do que é emitido, e aí deixa de servir para conferir.
func (s *Service) resolve(ctx context.Context, input EmitInput) (Resolved, error) {
	errs := domain.FieldErrors{}
	issuer, err := s.Repo.Issuer(ctx)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		// Primeiro uso com banco vazio: o dono ainda não configurou o emitente.
		errs["issuer"] = "configure o emitente em Configurações antes de emitir"
	case err != nil:
		return Resolved{}, fmt.Errorf("ler emitente: %w", err)
	}
	customer, err := s.Repo.Customer(ctx, input.CustomerID)
	if err != nil {
		errs["customer_id"] = "escolha o tomador"
	}
	template, err := s.Repo.Template(ctx, input.TemplateID)
	if err != nil {
		errs["template_id"] = "escolha o modelo de serviço"
	}
	amount, err := domain.ParseBRL(input.Amount)
	if err != nil || amount <= 0 {
		errs["amount"] = "informe um valor maior que zero, como 19.000,00"
	}
	competence, err := time.ParseInLocation("2006-01-02", input.Competence, s.Location)
	if err != nil {
		errs["competence"] = "informe a data de competência"
	}
	if strings.TrimSpace(input.Description) == "" {
		errs["description"] = "informe a descrição do serviço"
	}
	if len(errs) > 0 {
		return Resolved{}, errs
	}
	return Resolved{Issuer: issuer, Customer: customer, Template: template, Amount: amount, Competence: competence}, nil
}

// Preview monta o espelho da nota: as mesmas leituras e validações da emissão,
// e nada além disso. Não reserva número de DPS, não grava, não assina e não
// transmite — serve para o dono conferir ANTES de existir documento fiscal,
// porque nota emitida só se desfaz por cancelamento.
//
// A guarda de produção NÃO se aplica aqui: o espelho existe justamente para
// decidir se confirma, e exigir a confirmação antes de poder ver seria o
// contrário do propósito.
func (s *Service) Preview(ctx context.Context, input EmitInput) (Resolved, error) {
	return s.resolve(ctx, input)
}

func (s *Service) Emit(ctx context.Context, input EmitInput) (domain.Invoice, error) {
	if previous, found, err := s.byIdempotencyKey(ctx, input.IdempotencyKey); err != nil || found {
		return previous, err
	}
	resolved, err := s.resolve(ctx, input)
	if err != nil {
		return domain.Invoice{}, err
	}
	issuer, customer, template := resolved.Issuer, resolved.Customer, resolved.Template
	amount, competence := resolved.Amount, resolved.Competence
	// A guarda vem antes de reservar número: recusa não gasta DPS.
	if err := EnsureEnvironment(issuer.Environment, input.ConfirmProduction); err != nil {
		return domain.Invoice{}, err
	}

	series, number, err := s.Repo.ReserveDPSNumber(ctx)
	if err != nil {
		return domain.Invoice{}, fmt.Errorf("reservar número da DPS: %w", err)
	}

	invoice := domain.Invoice{
		Environment: issuer.Environment,
		Series:      series,
		DPSNumber:   number,
		CustomerID:  customer.ID,
		TemplateID:  template.ID,
		Amount:      amount,
		Competence:  competence,
		IssuedAt:    s.Now().In(s.Location),
		Description: strings.TrimSpace(input.Description),
		ServiceCode: template.NationalTaxCode,
		Status:      domain.StatusPending,

		ScheduleID:     input.ScheduleID,
		SchedulePeriod: input.SchedulePeriod,
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
	}
	// A nota é gravada pendente antes do envio: se o processo cair no meio, o
	// número reservado fica registrado e não é reutilizado.
	if err := s.Repo.SaveInvoice(ctx, &invoice); err != nil {
		return domain.Invoice{}, fmt.Errorf("gravar nota pendente: %w", err)
	}
	return s.send(ctx, issuer, customer, template, invoice)
}

// send leva a nota pendente ao emissor e grava a resposta. Serve à primeira
// tentativa e ao reenvio do agendador, que repete com o MESMO número de DPS:
// se a primeira tivesse chegado lá, a SEFIN responde duplicada em vez de
// gerar outra nota.
func (s *Service) send(ctx context.Context, issuer domain.Issuer, customer domain.Customer, template domain.Template, invoice domain.Invoice) (domain.Invoice, error) {
	result, err := s.Emitter.Emit(ctx, ports.EmitRequest{Issuer: issuer, Customer: customer, Template: template, Invoice: invoice})
	if err != nil {
		// Falha de comunicação: a nota continua pendente, com o motivo, para
		// ser consultada antes de qualquer reenvio.
		invoice.ErrorMessage = err.Error()
		return invoice, errors.Join(err, s.Repo.SaveInvoice(ctx, &invoice))
	}

	status, err := invoice.Status.TransitionTo(result.Status)
	if err != nil {
		return invoice, err
	}
	invoice.Status = status
	invoice.AccessKey = result.AccessKey
	invoice.NFSeNumber = result.NFSeNumber
	invoice.ErrorCode = result.ErrorCode
	invoice.ErrorMessage = result.ErrorMessage
	invoice.Simulated = result.Simulated
	invoice.XML = result.XML
	if err := s.Repo.SaveInvoice(ctx, &invoice); err != nil {
		return invoice, err
	}
	// Envio automático só depois de gravada: se o e-mail falhar, a nota
	// autorizada já está salva, e a falha fica registrada no detalhe dela.
	if invoice.Status == domain.StatusAuthorized {
		s.autoEmail(ctx, invoice)
	}
	return invoice, nil
}

// Resend tenta de novo uma nota que ficou pendente por falha de comunicação,
// com o mesmo número de DPS.
func (s *Service) Resend(ctx context.Context, invoice domain.Invoice, confirmProduction bool) (domain.Invoice, error) {
	if err := EnsureEnvironment(invoice.Environment, confirmProduction); err != nil {
		return invoice, err
	}
	if invoice.Status != domain.StatusPending {
		return invoice, fmt.Errorf("a nota está %q: só nota pendente é reenviada", invoice.Status)
	}
	issuer, err := s.Repo.Issuer(ctx)
	if err != nil {
		return invoice, err
	}
	customer, err := s.Repo.Customer(ctx, invoice.CustomerID)
	if err != nil {
		return invoice, err
	}
	template, err := s.Repo.Template(ctx, invoice.TemplateID)
	if err != nil {
		return invoice, err
	}
	return s.send(ctx, issuer, customer, template, invoice)
}

// InvoiceByIdempotencyKey acha a nota pela chave de idempotência que o
// integrador mandou na emissão (a API REST consulta por ela). Chave vazia ou
// sem nota devolve ports.ErrNotFound.
func (s *Service) InvoiceByIdempotencyKey(ctx context.Context, key string) (domain.Invoice, error) {
	invoice, found, err := s.byIdempotencyKey(ctx, key)
	if err == nil && !found {
		err = ports.ErrNotFound
	}
	return invoice, err
}

// byIdempotencyKey acha a nota já criada com a chave. Chave vazia nunca casa.
// A leitura é a via rápida; quem garante de fato é o índice único do banco,
// que recusa a gravação se duas chamadas iguais chegarem juntas.
func (s *Service) byIdempotencyKey(ctx context.Context, key string) (domain.Invoice, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return domain.Invoice{}, false, nil
	}
	invoices, err := s.Repo.Invoices(ctx)
	if err != nil {
		return domain.Invoice{}, false, err
	}
	for _, invoice := range invoices {
		if invoice.IdempotencyKey == key {
			return invoice, true, nil
		}
	}
	return domain.Invoice{}, false, nil
}
