package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// fakeCanceller devolve a resposta programada e guarda o evento recebido.
type fakeCanceller struct {
	result ports.CancelResult
	err    error
	got    domain.CancelEvent
}

// Cancel cumpre ports.Canceller para o teste.
func (f *fakeCanceller) Cancel(_ context.Context, event domain.CancelEvent) (ports.CancelResult, error) {
	f.got = event
	return f.result, f.err
}

// authorizedInvoice grava uma nota autorizada fictícia e devolve o serviço.
func authorizedInvoice(t *testing.T) (*Service, int64) {
	t.Helper()
	repo := memory.New(memory.DemoIssuer())
	invoice := domain.Invoice{Environment: domain.EnvHomologation, Status: domain.StatusAuthorized, AccessKey: "35057082112223330001810000000000003826100000000001", ErrorCode: "E9", ErrorMessage: "velho"}
	if err := repo.SaveInvoice(context.Background(), &invoice); err != nil {
		t.Fatal(err)
	}
	return &Service{Repo: repo, Now: fixedNow, Location: brt}, invoice.ID
}

// TestCancelRegistered: evento aceito vira nota cancelled e limpa o erro antigo.
func TestCancelRegistered(t *testing.T) {
	service, id := authorizedInvoice(t)
	canceller := &fakeCanceller{result: ports.CancelResult{Registered: true}}
	report, err := service.Cancel(context.Background(), canceller, id, "erro_emissao", "valor digitado errado", "v1", false)
	if err != nil || !report.Registered {
		t.Fatalf("cancelar: %+v, %v", report, err)
	}
	stored, _ := service.Repo.Invoice(context.Background(), id)
	if stored.Status != domain.StatusCancelled || stored.ErrorCode != "" || canceller.got.ReasonCode != "1" || canceller.got.At.Location() != brt {
		t.Fatalf("nota: %+v, evento: %+v", stored, canceller.got)
	}
}

// TestCancelRejected: recusa mantém authorized com o erro; recusa por prazo
// ganha a orientação de pedir na prefeitura.
func TestCancelRejected(t *testing.T) {
	for message, wantHint := range map[string]bool{"[E1] Prazo para cancelamento expirado": true, "[E2] Chave inexistente": false} {
		service, id := authorizedInvoice(t)
		canceller := &fakeCanceller{result: ports.CancelResult{ErrorCode: "E1", ErrorMessage: message}}
		report, err := service.Cancel(context.Background(), canceller, id, "outros", "justificativa comprida", "v1", false)
		if err != nil || report.Registered {
			t.Fatalf("%s: %+v, %v", message, report, err)
		}
		stored, _ := service.Repo.Invoice(context.Background(), id)
		if stored.Status != domain.StatusAuthorized || stored.ErrorCode != "E1" || strings.Contains(stored.ErrorMessage, "prefeitura") != wantHint {
			t.Fatalf("%s: %+v", message, stored)
		}
	}
}

// TestCancelFailures: nota inexistente, validação, rede e erro de gravação
// não mudam a nota.
func TestCancelFailures(t *testing.T) {
	ctx := context.Background()
	service, id := authorizedInvoice(t)
	if _, err := service.Cancel(ctx, &fakeCanceller{}, 999, "outros", "justificativa comprida", "v1", false); err == nil {
		t.Fatal("nota inexistente deveria falhar")
	}
	if _, err := service.Cancel(ctx, &fakeCanceller{}, id, "outros", "curta", "v1", false); err == nil {
		t.Fatal("justificativa curta deveria falhar")
	}
	offline := errors.New("rede fora")
	if _, err := service.Cancel(ctx, &fakeCanceller{err: offline}, id, "outros", "justificativa comprida", "v1", false); !errors.Is(err, offline) {
		t.Fatalf("rede: %v", err)
	}
	stored, _ := service.Repo.Invoice(ctx, id)
	if stored.Status != domain.StatusAuthorized {
		t.Fatalf("nota mudou: %+v", stored)
	}
	noIssuer := &Service{Repo: issuerFailRepo{service.Repo}, Now: fixedNow, Location: brt}
	if _, err := noIssuer.Cancel(ctx, &fakeCanceller{}, id, "outros", "justificativa comprida", "v1", false); err == nil {
		t.Fatal("sem emitente deveria falhar")
	}
	saveFail := &Service{Repo: saveFailRepo{service.Repo}, Now: fixedNow, Location: brt}
	if _, err := saveFail.Cancel(ctx, &fakeCanceller{result: ports.CancelResult{Registered: true}}, id, "outros", "justificativa comprida", "v1", false); err == nil {
		t.Fatal("falha ao gravar deveria aparecer")
	}
}

// issuerFailRepo falha ao ler o emitente.
type issuerFailRepo struct{ ports.Repository }

// Issuer devolve erro fixo.
func (issuerFailRepo) Issuer(context.Context) (domain.Issuer, error) {
	return domain.Issuer{}, errors.New("sem emitente")
}

// saveFailRepo falha ao gravar a nota.
type saveFailRepo struct{ ports.Repository }

// SaveInvoice devolve erro fixo.
func (saveFailRepo) SaveInvoice(context.Context, *domain.Invoice) error {
	return errors.New("disco cheio")
}
