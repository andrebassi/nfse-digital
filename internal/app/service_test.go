package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/adapters/simulated"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// brt é UTC-3 fixo, para o teste não depender do tzdata da máquina.
var brt = time.FixedZone("BRT", -3*60*60)

// fixedNow devolve sempre 29/10/2026 22:52, o momento das medições de referência.
func fixedNow() time.Time { return time.Date(2026, 10, 29, 22, 52, 0, 0, brt) }

// newService monta o caso de uso com o repositório real em memória e o emissor dado.
func newService(emitter ports.Emitter) (*Service, *memory.Repository) {
	repo := memory.NewSeeded(brt)
	return &Service{Repo: repo, Emitter: emitter, Now: fixedNow, Location: brt}, repo
}

// demoInput monta o pedido de emissão da Ipê com os IDs do seed.
func demoInput(t *testing.T, repo *memory.Repository) EmitInput {
	t.Helper()
	ctx := context.Background()
	customers, _ := repo.Customers(ctx, "ipe")
	templates, _ := repo.Templates(ctx, "ipe")
	return EmitInput{CustomerID: customers[0].ID, TemplateID: templates[0].ID, Amount: "19.000,00", Competence: "2026-10-29", Description: "Referente a outubro"}
}

// TestInvoiceByIdempotencyKey: acha a nota pela chave da emissão; chave
// vazia ou desconhecida é ErrNotFound.
func TestInvoiceByIdempotencyKey(t *testing.T) {
	service, repo := newService(simulated.Emitter{})
	ctx := context.Background()
	input := demoInput(t, repo)
	input.IdempotencyKey = "fatura-42"
	emitted, _ := service.Emit(ctx, input)
	found, err := service.InvoiceByIdempotencyKey(ctx, " fatura-42 ")
	if err != nil || found.ID != emitted.ID || found.ID == 0 {
		t.Fatalf("chave conhecida: %+v, %v", found, err)
	}
	for _, key := range []string{"", "outra"} {
		if _, err := service.InvoiceByIdempotencyKey(ctx, key); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("chave %q: %v", key, err)
		}
	}
}

// TestEmitBarueriEndsRejected: o fluxo inteiro com o emissor simulado termina
// como a SEFIN real terminou, rejeitada com E0039, e consome o número 1001.
func TestEmitBarueriEndsRejected(t *testing.T) {
	service, repo := newService(simulated.Emitter{})
	invoice, err := service.Emit(context.Background(), demoInput(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Status != domain.StatusRejected || invoice.ErrorCode != "E0039" || invoice.DPSNumber != 1001 || invoice.Amount != 1900000 {
		t.Fatalf("nota: %+v", invoice)
	}
	stored, _ := repo.Invoice(context.Background(), invoice.ID)
	if stored.Status != domain.StatusRejected || !stored.Simulated {
		t.Fatalf("gravada: %+v", stored)
	}
}

// TestEmitOtherCityAuthorized: emitente fora de Barueri sai autorizado (simulado).
func TestEmitOtherCityAuthorized(t *testing.T) {
	service, repo := newService(simulated.Emitter{})
	issuer, _ := repo.Issuer(context.Background())
	issuer.CityCode = "3550308"
	_ = repo.SaveIssuer(context.Background(), issuer)
	invoice, err := service.Emit(context.Background(), demoInput(t, repo))
	if err != nil || invoice.Status != domain.StatusAuthorized || invoice.AccessKey == "" {
		t.Fatalf("nota: %+v, %v", invoice, err)
	}
}

// TestEmitValidationDoesNotSpendNumber: entrada errada devolve todos os erros
// de campo e não consome número de DPS nem cria nota.
func TestEmitValidationDoesNotSpendNumber(t *testing.T) {
	service, repo := newService(simulated.Emitter{})
	_, err := service.Emit(context.Background(), EmitInput{Amount: "abc", Competence: "29/10/2026"})
	var fields domain.FieldErrors
	if !errors.As(err, &fields) {
		t.Fatalf("erro: %v", err)
	}
	for _, field := range []string{"customer_id", "template_id", "amount", "competence", "description"} {
		if fields[field] == "" {
			t.Errorf("faltou erro em %s", field)
		}
	}
	issuer, _ := repo.Issuer(context.Background())
	invoices, _ := repo.Invoices(context.Background())
	if issuer.NextDPSNumber != 1001 || len(invoices) != 5 {
		t.Fatal("validação gastou número ou criou nota")
	}
}

// failingEmitter simula queda de comunicação com a SEFIN.
type failingEmitter struct{}

// Emit sempre falha, como um timeout.
func (failingEmitter) Emit(context.Context, ports.EmitRequest) (ports.EmitResult, error) {
	return ports.EmitResult{}, errors.New("timeout")
}

// TestEmitTransientFailureKeepsPending: sem resposta, a nota fica pendente com
// o motivo, para ser consultada antes de reenviar.
func TestEmitTransientFailureKeepsPending(t *testing.T) {
	service, repo := newService(failingEmitter{})
	invoice, err := service.Emit(context.Background(), demoInput(t, repo))
	if err == nil || invoice.Status != domain.StatusPending {
		t.Fatalf("nota: %+v, %v", invoice, err)
	}
	stored, _ := repo.Invoice(context.Background(), invoice.ID)
	if stored.Status != domain.StatusPending || stored.ErrorMessage != "timeout" {
		t.Fatalf("gravada: %+v", stored)
	}
}

// strangeEmitter devolve um estado que não pode vir de uma nota pendente.
type strangeEmitter struct{}

// Emit devolve "cancelada", que pendente não pode virar.
func (strangeEmitter) Emit(context.Context, ports.EmitRequest) (ports.EmitResult, error) {
	return ports.EmitResult{Status: domain.StatusCancelled}, nil
}

// TestEmitRejectsImpossibleTransition: resposta incoerente do emissor vira erro.
func TestEmitRejectsImpossibleTransition(t *testing.T) {
	service, repo := newService(strangeEmitter{})
	if _, err := service.Emit(context.Background(), demoInput(t, repo)); err == nil {
		t.Fatal("transição pendente→cancelada deveria falhar")
	}
}

// brokenRepo falha onde o teste mandar, para cobrir os erros de I/O do caso de uso.
type brokenRepo struct {
	*memory.Repository
	failIssuer, failReserve, failSave bool
}

// Issuer falha quando failIssuer está ligado.
func (b brokenRepo) Issuer(ctx context.Context) (domain.Issuer, error) {
	if b.failIssuer {
		return domain.Issuer{}, errors.New("disco")
	}
	return b.Repository.Issuer(ctx)
}

// ReserveDPSNumber falha quando failReserve está ligado.
func (b brokenRepo) ReserveDPSNumber(ctx context.Context) (string, int64, error) {
	if b.failReserve {
		return "", 0, errors.New("disco")
	}
	return b.Repository.ReserveDPSNumber(ctx)
}

// SaveInvoice falha quando failSave está ligado.
func (b brokenRepo) SaveInvoice(ctx context.Context, invoice *domain.Invoice) error {
	if b.failSave {
		return errors.New("disco")
	}
	return b.Repository.SaveInvoice(ctx, invoice)
}

// TestEmitRepositoryFailures: cada falha de I/O vira erro, sem pânico.
func TestEmitRepositoryFailures(t *testing.T) {
	for _, broken := range []brokenRepo{{failIssuer: true}, {failReserve: true}, {failSave: true}} {
		repo := memory.NewSeeded(brt)
		broken.Repository = repo
		service := &Service{Repo: broken, Emitter: simulated.Emitter{}, Now: fixedNow, Location: brt}
		if _, err := service.Emit(context.Background(), demoInput(t, repo)); err == nil {
			t.Errorf("%+v deveria falhar", broken)
		}
	}
}
