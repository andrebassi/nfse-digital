package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// scriptedEmitter devolve, em ordem, os erros programados (nil = autoriza) e
// conta as chamadas com o número de DPS de cada uma.
type scriptedEmitter struct {
	errs    []error
	numbers []int64
}

// Emit cumpre ports.Emitter para o teste.
func (e *scriptedEmitter) Emit(_ context.Context, request ports.EmitRequest) (ports.EmitResult, error) {
	e.numbers = append(e.numbers, request.Invoice.DPSNumber)
	var err error
	if len(e.errs) > 0 {
		err, e.errs = e.errs[0], e.errs[1:]
	}
	if err != nil {
		return ports.EmitResult{}, err
	}
	return ports.EmitResult{Status: domain.StatusAuthorized, AccessKey: "K", NFSeNumber: "1"}, nil
}

// fakeNotifier guarda os avisos e pode falhar.
type fakeNotifier struct {
	messages []string
	err      error
}

// Notify cumpre ports.Notifier para o teste.
func (n *fakeNotifier) Notify(_ context.Context, title, message string) error {
	n.messages = append(n.messages, title+": "+message)
	return n.err
}

// schedulerFixture monta emitente, tomador, modelo e um agendamento dia 28
// com competência no mês anterior.
func schedulerFixture(t *testing.T, issuer domain.Issuer, schedule domain.Schedule) (*Scheduler, *memory.Repository, *scriptedEmitter) {
	t.Helper()
	ctx := context.Background()
	repo := memory.New(issuer)
	customer := domain.Customer{Document: "56789012000100", Name: "IPÊ INFORMÁTICA LTDA"}
	template := domain.Template{Name: "Consultoria", Description: "Serviço de {mes}/{ano}", NationalTaxCode: "010701"}
	_ = repo.SaveCustomer(ctx, &customer)
	_ = repo.SaveTemplate(ctx, &template)
	schedule.CustomerID, schedule.TemplateID = customer.ID, template.ID
	if err := repo.SaveSchedule(ctx, &schedule); err != nil {
		t.Fatal(err)
	}
	emitter := &scriptedEmitter{}
	service := &Service{Repo: repo, Emitter: emitter, Now: fixedNow, Location: brt}
	return &Scheduler{Service: service}, repo, emitter
}

// monthlySchedule é o agendamento padrão dos testes.
func monthlySchedule() domain.Schedule {
	return domain.Schedule{Amount: 1900000, Day: 28, Competence: domain.CompetencePreviousMonth, Active: true, NotifyEve: true}
}

// at monta um instante de novembro de 2026 em Brasília.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, 11, day, hour, minute, 0, 0, brt)
}

// TestSchedulerEmitsOncePerMonth: antes das 03:00 nada; às 03:00 emite com a
// competência de outubro e a descrição resolvida; a rodada do minuto seguinte
// não emite de novo.
func TestSchedulerEmitsOncePerMonth(t *testing.T) {
	scheduler, repo, emitter := schedulerFixture(t, memory.DemoIssuer(), monthlySchedule())
	ctx := context.Background()
	if outcomes, _ := scheduler.Run(ctx, at(28, 2, 59), false); len(outcomes) != 0 {
		t.Fatalf("antes da hora: %+v", outcomes)
	}
	outcomes, err := scheduler.Run(ctx, at(28, 3, 0), false)
	if err != nil || len(outcomes) != 1 || outcomes[0].Action != ActionEmitted || outcomes[0].Err != nil {
		t.Fatalf("emissão: %+v, %v", outcomes, err)
	}
	invoice := outcomes[0].Invoice
	if invoice.Status != domain.StatusAuthorized || invoice.SchedulePeriod != "2026-11" || domain.PeriodKey(invoice.Competence) != "2026-10" ||
		invoice.Description != "Serviço de outubro/2026" || invoice.Amount != 1900000 {
		t.Fatalf("nota: %+v", invoice)
	}
	if outcomes, _ := scheduler.Run(ctx, at(28, 3, 1), false); len(outcomes) != 0 || len(emitter.numbers) != 1 {
		t.Fatalf("segunda rodada: %+v, %d envios", outcomes, len(emitter.numbers))
	}
	invoices, _ := repo.Invoices(ctx)
	if len(invoices) != 1 {
		t.Fatalf("%d notas", len(invoices))
	}
}

// TestSchedulerRetriesHourlySameDPS: falha de comunicação deixa pendente;
// meia hora depois não tenta; uma hora depois tenta com o MESMO número; no
// dia seguinte não tenta mais.
func TestSchedulerRetriesHourlySameDPS(t *testing.T) {
	scheduler, _, emitter := schedulerFixture(t, memory.DemoIssuer(), monthlySchedule())
	ctx := context.Background()
	offline := errors.New("SEFIN fora do ar")
	emitter.errs = []error{offline, offline}
	scheduler.Service.Now = func() time.Time { return at(28, 3, 0) }

	outcomes, _ := scheduler.Run(ctx, at(28, 3, 0), false)
	if len(outcomes) != 1 || !errors.Is(outcomes[0].Err, offline) || outcomes[0].Invoice.Status != domain.StatusPending {
		t.Fatalf("primeira: %+v", outcomes)
	}
	if outcomes, _ := scheduler.Run(ctx, at(28, 3, 30), false); len(outcomes) != 0 {
		t.Fatalf("meia hora: %+v", outcomes)
	}
	if outcomes, _ := scheduler.Run(ctx, at(28, 4, 0), false); len(outcomes) != 1 || outcomes[0].Action != ActionRetried {
		t.Fatalf("uma hora: %+v", outcomes)
	}
	outcomes, _ = scheduler.Run(ctx, at(28, 5, 0), false)
	if len(outcomes) != 1 || outcomes[0].Invoice.Status != domain.StatusAuthorized {
		t.Fatalf("terceira: %+v", outcomes)
	}
	if len(emitter.numbers) != 3 || emitter.numbers[0] != emitter.numbers[1] || emitter.numbers[1] != emitter.numbers[2] {
		t.Fatalf("números enviados: %v", emitter.numbers)
	}

	// Outro mês, sempre fora do ar: depois da meia-noite para de tentar.
	scheduler2, _, emitter2 := schedulerFixture(t, memory.DemoIssuer(), monthlySchedule())
	emitter2.errs = []error{offline, offline, offline}
	scheduler2.Service.Now = func() time.Time { return at(28, 23, 0) }
	_, _ = scheduler2.Run(ctx, at(28, 23, 0), false)
	if outcomes, _ := scheduler2.Run(ctx, at(29, 1, 0), false); len(outcomes) != 0 || len(emitter2.numbers) != 1 {
		t.Fatalf("dia seguinte: %+v", outcomes)
	}
}

// TestSchedulerProductionGuard: emitente em produção só emite sozinho com a
// permissão gravada no agendamento.
func TestSchedulerProductionGuard(t *testing.T) {
	production := memory.DemoIssuer()
	production.Environment = domain.EnvProduction
	scheduler, _, emitter := schedulerFixture(t, production, monthlySchedule())
	outcomes, _ := scheduler.Run(context.Background(), at(28, 3, 0), false)
	if len(outcomes) != 1 || outcomes[0].Action != ActionBlockedProduction || len(emitter.numbers) != 0 {
		t.Fatalf("sem permissão: %+v", outcomes)
	}
	allowed := monthlySchedule()
	allowed.Production = true
	scheduler, _, emitter = schedulerFixture(t, production, allowed)
	if outcomes, _ := scheduler.Run(context.Background(), at(28, 3, 0), false); len(outcomes) != 1 || outcomes[0].Action != ActionEmitted || len(emitter.numbers) != 1 {
		t.Fatalf("com permissão: %+v", outcomes)
	}
}

// TestSchedulerDryRun relata sem emitir nem marcar aviso.
func TestSchedulerDryRun(t *testing.T) {
	scheduler, repo, emitter := schedulerFixture(t, memory.DemoIssuer(), monthlySchedule())
	notifier := &fakeNotifier{}
	scheduler.Notifier = notifier
	ctx := context.Background()
	if outcomes, _ := scheduler.Run(ctx, at(28, 3, 0), true); len(outcomes) != 1 || outcomes[0].Action != ActionWouldEmit {
		t.Fatalf("emissão: %+v", outcomes)
	}
	if outcomes, _ := scheduler.Run(ctx, at(27, 9, 0), true); len(outcomes) != 1 || outcomes[0].Action != ActionWouldNotify {
		t.Fatalf("aviso: %+v", outcomes)
	}
	invoices, _ := repo.Invoices(ctx)
	schedules, _ := repo.Schedules(ctx)
	if len(invoices) != 0 || len(emitter.numbers) != 0 || len(notifier.messages) != 0 || schedules[0].NotifiedPeriod != "" {
		t.Fatalf("dry-run mexeu: %d notas, %d envios, %v, %+v", len(invoices), len(emitter.numbers), notifier.messages, schedules[0])
	}
}

// TestSchedulerEveNotice: aviso uma vez só por mês, com valor e competência;
// falha do envio é relatada; sem notificador, nada.
func TestSchedulerEveNotice(t *testing.T) {
	scheduler, _, _ := schedulerFixture(t, memory.DemoIssuer(), monthlySchedule())
	ctx := context.Background()
	if outcomes, _ := scheduler.Run(ctx, at(27, 9, 0), false); len(outcomes) != 0 {
		t.Fatalf("sem notificador: %+v", outcomes)
	}
	notifier := &fakeNotifier{}
	scheduler.Notifier = notifier
	outcomes, _ := scheduler.Run(ctx, at(27, 9, 0), false)
	if len(outcomes) != 1 || outcomes[0].Action != ActionNotified || len(notifier.messages) != 1 ||
		!strings.Contains(notifier.messages[0], "R$ 19.000,00") || !strings.Contains(notifier.messages[0], "competência 10/2026") {
		t.Fatalf("aviso: %+v %v", outcomes, notifier.messages)
	}
	if outcomes, _ := scheduler.Run(ctx, at(27, 9, 1), false); len(outcomes) != 0 || len(notifier.messages) != 1 {
		t.Fatalf("aviso repetido: %+v", outcomes)
	}

	failing, _, _ := schedulerFixture(t, memory.DemoIssuer(), monthlySchedule())
	failing.Notifier = &fakeNotifier{err: errors.New("ntfy fora")}
	if outcomes, _ := failing.Run(ctx, at(27, 10, 0), false); len(outcomes) != 1 || outcomes[0].Action != ActionNotifyFailed || outcomes[0].Err == nil {
		t.Fatalf("falha do aviso: %+v", outcomes)
	}
}

// staleRepo esconde as notas na leitura, como se outro processo tivesse
// acabado de gravar a do mês: só o índice único segura a duplicata.
type staleRepo struct{ *memory.Repository }

// Invoices devolve sempre vazio.
func (staleRepo) Invoices(context.Context) ([]domain.Invoice, error) { return nil, nil }

// TestSchedulerConflictAndErrors: corrida com outro processo vira
// already-emitted; falha ao ler emitente, agendamentos ou notas vira erro.
func TestSchedulerConflictAndErrors(t *testing.T) {
	scheduler, repo, _ := schedulerFixture(t, memory.DemoIssuer(), monthlySchedule())
	ctx := context.Background()
	_, _ = scheduler.Run(ctx, at(28, 3, 0), false)
	scheduler.Service.Repo = staleRepo{repo}
	if outcomes, _ := scheduler.Run(ctx, at(28, 4, 0), false); len(outcomes) != 1 || outcomes[0].Action != ActionAlreadyEmitted {
		t.Fatalf("corrida: %+v", outcomes)
	}

	if _, err := (&Scheduler{Service: &Service{Repo: issuerFailRepo{repo}}}).Run(ctx, at(28, 3, 0), false); err == nil {
		t.Fatal("sem emitente deveria falhar")
	}
	if _, err := (&Scheduler{Service: &Service{Repo: listFailRepo{Repository: repo, schedules: true}}}).Run(ctx, at(28, 3, 0), false); err == nil {
		t.Fatal("falha nos agendamentos deveria aparecer")
	}
	if _, err := (&Scheduler{Service: &Service{Repo: listFailRepo{Repository: repo}}}).Run(ctx, at(28, 3, 0), false); err == nil {
		t.Fatal("falha nas notas deveria aparecer")
	}
	// A busca pela chave de idempotência lê as notas: falha ali não pode virar emissão.
	if _, err := (&Service{Repo: listFailRepo{Repository: repo}}).Emit(ctx, EmitInput{IdempotencyKey: "k"}); err == nil {
		t.Fatal("falha ao procurar a chave deveria aparecer")
	}
}

// TestSchedulerTemplateMissingAndResendErrors: agendamento sem descrição e
// com modelo apagado relata erro; Resend recusa nota que não está pendente e
// propaga falha de leitura.
func TestSchedulerTemplateMissingAndResendErrors(t *testing.T) {
	scheduler, repo, _ := schedulerFixture(t, memory.DemoIssuer(), monthlySchedule())
	ctx := context.Background()
	scheduler.Service.Repo = templateFailRepo{repo}
	outcomes, _ := scheduler.Run(ctx, at(28, 3, 0), false)
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Fatalf("modelo apagado: %+v", outcomes)
	}
	service := scheduler.Service
	if _, err := service.Resend(ctx, domain.Invoice{Status: domain.StatusAuthorized}, false); err == nil {
		t.Fatal("reenviar autorizada deveria falhar")
	}
	pending := domain.Invoice{Status: domain.StatusPending, CustomerID: 999}
	service.Repo = repo
	if _, err := service.Resend(ctx, pending, false); err == nil {
		t.Fatal("tomador inexistente deveria falhar")
	}
	schedules, _ := repo.Schedules(ctx)
	pending.CustomerID = schedules[0].CustomerID
	pending.TemplateID = 999
	if _, err := service.Resend(ctx, pending, false); err == nil {
		t.Fatal("modelo inexistente deveria falhar")
	}
	service.Repo = issuerFailRepo{repo}
	if _, err := service.Resend(ctx, pending, false); err == nil {
		t.Fatal("sem emitente deveria falhar")
	}
}

// listFailRepo falha ao listar agendamentos (schedules=true) ou notas.
type listFailRepo struct {
	ports.Repository
	schedules bool
}

// Schedules falha quando configurado.
func (r listFailRepo) Schedules(ctx context.Context) ([]domain.Schedule, error) {
	if r.schedules {
		return nil, errors.New("falha")
	}
	return r.Repository.Schedules(ctx)
}

// Invoices falha sempre.
func (listFailRepo) Invoices(context.Context) ([]domain.Invoice, error) {
	return nil, errors.New("falha")
}

// templateFailRepo finge que o modelo foi apagado.
type templateFailRepo struct{ ports.Repository }

// Template devolve não encontrado.
func (templateFailRepo) Template(context.Context, int64) (domain.Template, error) {
	return domain.Template{}, ports.ErrNotFound
}
