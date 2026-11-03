package app

import (
	"context"
	"errors"
	"testing"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// trapEmitter conta chamadas por ambiente: chegar aqui em produção sem
// confirmação é exatamente o defeito que a guarda existe para impedir.
type trapEmitter struct{ production int }

// Emit registra a chamada e autoriza.
func (e *trapEmitter) Emit(_ context.Context, request ports.EmitRequest) (ports.EmitResult, error) {
	if request.Invoice.Environment == domain.EnvProduction {
		e.production++
	}
	return ports.EmitResult{Status: domain.StatusAuthorized, AccessKey: "K"}, nil
}

// trapCanceller conta pedidos de cancelamento em produção.
type trapCanceller struct{ production int }

// Cancel registra a chamada e aceita.
func (c *trapCanceller) Cancel(_ context.Context, event domain.CancelEvent) (ports.CancelResult, error) {
	if event.Environment == domain.EnvProduction {
		c.production++
	}
	return ports.CancelResult{Registered: true}, nil
}

// channel é um caminho que fala com a SEFIN em nome do dono. Cada um recebe
// o repositório preparado e a confirmação, e devolve o erro da guarda.
type channel struct {
	name string
	run  func(t *testing.T, service *Service, emitter *trapEmitter, canceller *trapCanceller, confirmed bool) error
}

// channels são os quatro canais da story 4.1. CLI, painel e MCP entram por
// Service.Emit com ConfirmProduction; o agendador, pela permissão gravada.
var channels = []channel{
	{"emitir (CLI, painel, MCP)", func(t *testing.T, service *Service, _ *trapEmitter, _ *trapCanceller, confirmed bool) error {
		customers, _ := service.Repo.Customers(context.Background(), "")
		templates, _ := service.Repo.Templates(context.Background(), "")
		_, err := service.Emit(context.Background(), EmitInput{CustomerID: customers[0].ID, TemplateID: templates[0].ID, Amount: "1,00",
			Competence: "2026-10-29", Description: "teste", ConfirmProduction: confirmed})
		return err
	}},
	{"agendador", func(t *testing.T, service *Service, _ *trapEmitter, _ *trapCanceller, confirmed bool) error {
		customers, _ := service.Repo.Customers(context.Background(), "")
		templates, _ := service.Repo.Templates(context.Background(), "")
		schedule := domain.Schedule{CustomerID: customers[0].ID, TemplateID: templates[0].ID, Amount: 100, Day: 1,
			Competence: domain.CompetenceEmission, Description: "teste", Active: true, Production: confirmed}
		_ = service.Repo.SaveSchedule(context.Background(), &schedule)
		outcomes, err := (&Scheduler{Service: service}).Run(context.Background(), at(28, 3, 0), false)
		if err == nil && len(outcomes) > 0 && outcomes[0].Action == ActionBlockedProduction {
			return ErrProductionNotConfirmed
		}
		return err
	}},
	{"reenviar pendente", func(t *testing.T, service *Service, _ *trapEmitter, _ *trapCanceller, confirmed bool) error {
		issuer, _ := service.Repo.Issuer(context.Background())
		customers, _ := service.Repo.Customers(context.Background(), "")
		templates, _ := service.Repo.Templates(context.Background(), "")
		pending := domain.Invoice{Environment: issuer.Environment, Status: domain.StatusPending, CustomerID: customers[0].ID, TemplateID: templates[0].ID}
		_ = service.Repo.SaveInvoice(context.Background(), &pending)
		_, err := service.Resend(context.Background(), pending, confirmed)
		return err
	}},
	{"cancelar", func(t *testing.T, service *Service, _ *trapEmitter, canceller *trapCanceller, confirmed bool) error {
		issuer, _ := service.Repo.Issuer(context.Background())
		invoice := domain.Invoice{Environment: issuer.Environment, Status: domain.StatusAuthorized, AccessKey: "35057082112223330001810000000000003826100000000001"}
		_ = service.Repo.SaveInvoice(context.Background(), &invoice)
		_, err := service.Cancel(context.Background(), canceller, invoice.ID, "outros", "justificativa comprida", "v1", confirmed)
		return err
	}},
}

// TestProductionGuardEveryChannel percorre canal × ambiente × confirmação:
// em produção sem confirmação nenhum canal chega ao adapter; com ela, chega;
// em homologação a confirmação não importa. Remover a chamada a
// EnsureEnvironment de qualquer caso de uso faz este teste reprovar.
func TestProductionGuardEveryChannel(t *testing.T) {
	for _, ch := range channels {
		for _, environment := range []domain.Environment{domain.EnvHomologation, domain.EnvProduction} {
			for _, confirmed := range []bool{false, true} {
				issuer := memory.DemoIssuer()
				issuer.Environment = environment
				repo := memory.New(issuer)
				customer := domain.Customer{Document: "56789012000100", Name: "IPÊ INFORMÁTICA LTDA"}
				template := domain.Template{Name: "Consultoria", NationalTaxCode: "010701"}
				_ = repo.SaveCustomer(context.Background(), &customer)
				_ = repo.SaveTemplate(context.Background(), &template)
				emitter, canceller := &trapEmitter{}, &trapCanceller{}
				service := &Service{Repo: repo, Emitter: emitter, Now: fixedNow, Location: brt}

				err := ch.run(t, service, emitter, canceller, confirmed)
				reached := emitter.production + canceller.production
				blocked := environment == domain.EnvProduction && !confirmed
				label := ch.name + "/" + string(environment)
				if blocked && (!errors.Is(err, ErrProductionNotConfirmed) || reached != 0) {
					t.Errorf("%s sem confirmação: err=%v, chegou ao adapter %d vez(es)", label, err, reached)
				}
				if !blocked && errors.Is(err, ErrProductionNotConfirmed) {
					t.Errorf("%s confirmado=%v: recusado indevidamente", label, confirmed)
				}
				if environment == domain.EnvProduction && confirmed && reached != 1 {
					t.Errorf("%s confirmado: chegou ao adapter %d vez(es)", label, reached)
				}
			}
		}
	}
}
