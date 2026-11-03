package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// Ações que o agendador relata por agendamento em cada rodada.
const (
	ActionEmitted           = "emitted"
	ActionRetried           = "retried"
	ActionWouldEmit         = "would-emit"
	ActionBlockedProduction = "blocked-production"
	ActionAlreadyEmitted    = "already-emitted"
	ActionNotified          = "notified"
	ActionWouldNotify       = "would-notify"
	ActionNotifyFailed      = "notify-failed"
)

// DefaultRetryEvery é o intervalo entre tentativas de uma nota que ficou
// pendente por falha de comunicação: a SEFIN fora do ar costuma voltar em
// minutos, e uma por hora não martela o serviço nem atrasa o dia.
const DefaultRetryEvery = time.Hour

// ScheduleOutcome é o que aconteceu com um agendamento numa rodada.
type ScheduleOutcome struct {
	ScheduleID int64
	Period     string
	Action     string
	Invoice    domain.Invoice
	Err        error
}

// Scheduler roda os agendamentos. Run é chamado a cada minuto pelo serve; a
// garantia de "uma nota por mês" não depende de memória do processo, e sim do
// índice único (agendamento, mês) no banco.
type Scheduler struct {
	Service    *Service
	Notifier   ports.Notifier
	RetryEvery time.Duration

	mu sync.Mutex
	// lastTry guarda, por nota, quando foi a última tentativa. Some ao
	// reiniciar, e aí a pendente é tentada de novo na primeira rodada.
	lastTry map[int64]time.Time
}

// Run avalia todos os agendamentos em `now`. Com dryRun, só relata o que
// faria: não emite, não reenvia e não marca aviso.
func (s *Scheduler) Run(ctx context.Context, now time.Time, dryRun bool) ([]ScheduleOutcome, error) {
	repo := s.Service.Repo
	issuer, err := repo.Issuer(ctx)
	if err != nil {
		return nil, fmt.Errorf("ler emitente: %w", err)
	}
	schedules, err := repo.Schedules(ctx)
	if err != nil {
		return nil, err
	}
	invoices, err := repo.Invoices(ctx)
	if err != nil {
		return nil, err
	}
	outcomes := []ScheduleOutcome{}
	for _, schedule := range schedules {
		if outcome, ok := s.runDue(ctx, issuer, schedule, invoices, now, dryRun); ok {
			outcomes = append(outcomes, outcome)
		}
		if outcome, ok := s.runEve(ctx, schedule, now, dryRun); ok {
			outcomes = append(outcomes, outcome)
		}
	}
	return outcomes, nil
}

// runDue emite a nota do mês, se venceu e ainda não saiu, ou reenvia a que
// ficou pendente por falha de comunicação (só no próprio dia, de hora em hora).
func (s *Scheduler) runDue(ctx context.Context, issuer domain.Issuer, schedule domain.Schedule, invoices []domain.Invoice, now time.Time, dryRun bool) (ScheduleOutcome, bool) {
	location := s.Service.Location
	run, due := schedule.DueRun(now, location)
	if !due {
		return ScheduleOutcome{}, false
	}
	outcome := ScheduleOutcome{ScheduleID: schedule.ID, Period: domain.PeriodKey(run.EmitAt)}
	if existing, found := findScheduled(invoices, schedule.ID, outcome.Period); found {
		return s.retry(ctx, outcome, existing, schedule, now, dryRun)
	}
	// Guarda da 4.1 no agendamento: emitente em produção só emite sozinho se
	// o dono gravou essa permissão na criação do agendamento.
	if issuer.Environment == domain.EnvProduction && !schedule.Production {
		outcome.Action = ActionBlockedProduction
		return outcome, true
	}
	if dryRun {
		outcome.Action = ActionWouldEmit
		return outcome, true
	}
	description := schedule.Description
	if description == "" {
		template, err := s.Service.Repo.Template(ctx, schedule.TemplateID)
		if err != nil {
			outcome.Err = err
			return outcome, true
		}
		description = template.Description
	}
	invoice, err := s.Service.Emit(ctx, EmitInput{
		CustomerID: schedule.CustomerID, TemplateID: schedule.TemplateID, Amount: schedule.Amount.InputValue(),
		Competence: run.Competence.Format("2006-01-02"), Description: domain.RenderDescription(description, run.Competence),
		ScheduleID: schedule.ID, SchedulePeriod: outcome.Period, ConfirmProduction: schedule.Production,
	})
	if errors.Is(err, ports.ErrConflict) {
		// Outro processo emitiu este mês entre a leitura e a gravação.
		outcome.Action = ActionAlreadyEmitted
		return outcome, true
	}
	if invoice.ID > 0 {
		s.markTry(invoice.ID, now)
	}
	outcome.Action, outcome.Invoice, outcome.Err = ActionEmitted, invoice, err
	if schedule.SendToCustomer {
		s.Service.scheduledEmail(ctx, invoice)
	}
	return outcome, true
}

// retry reenvia a nota pendente do mês com o mesmo número de DPS, se ainda é
// o dia da emissão e já passou o intervalo desde a última tentativa.
func (s *Scheduler) retry(ctx context.Context, outcome ScheduleOutcome, invoice domain.Invoice, schedule domain.Schedule, now time.Time, dryRun bool) (ScheduleOutcome, bool) {
	if invoice.Status != domain.StatusPending || !sameDay(invoice.IssuedAt, now, s.Service.Location) {
		return ScheduleOutcome{}, false
	}
	s.mu.Lock()
	last, tried := s.lastTry[invoice.ID]
	s.mu.Unlock()
	if tried && now.Sub(last) < s.retryEvery() {
		return ScheduleOutcome{}, false
	}
	outcome.Action, outcome.Invoice = ActionRetried, invoice
	if dryRun {
		return outcome, true
	}
	s.markTry(invoice.ID, now)
	outcome.Invoice, outcome.Err = s.Service.Resend(ctx, invoice, schedule.Production)
	if schedule.SendToCustomer {
		s.Service.scheduledEmail(ctx, outcome.Invoice)
	}
	return outcome, true
}

// runEve manda o aviso da véspera uma vez por mês. A marca vai ao banco antes
// do envio: aviso perdido é melhor que aviso repetido a cada minuto.
func (s *Scheduler) runEve(ctx context.Context, schedule domain.Schedule, now time.Time, dryRun bool) (ScheduleOutcome, bool) {
	run, ok := schedule.EveRun(now, s.Service.Location)
	if !ok || s.Notifier == nil {
		return ScheduleOutcome{}, false
	}
	outcome := ScheduleOutcome{ScheduleID: schedule.ID, Period: domain.PeriodKey(run.EmitAt)}
	if schedule.NotifiedPeriod == outcome.Period {
		return ScheduleOutcome{}, false
	}
	if dryRun {
		outcome.Action = ActionWouldNotify
		return outcome, true
	}
	claimed, err := s.Service.Repo.ClaimNotification(ctx, schedule.ID, outcome.Period)
	if err != nil || !claimed {
		outcome.Action, outcome.Err = ActionNotifyFailed, err
		return outcome, err != nil
	}
	message := fmt.Sprintf("Amanhã às %s sai a nota do agendamento #%d: %s, competência %s.",
		run.EmitAt.Format("15:04"), schedule.ID, schedule.Amount.FormatBRL(), run.Competence.Format("01/2006"))
	if err := s.Notifier.Notify(ctx, "NFS-e agendada para amanhã", message); err != nil {
		outcome.Action, outcome.Err = ActionNotifyFailed, err
		return outcome, true
	}
	outcome.Action = ActionNotified
	return outcome, true
}

// markTry registra a tentativa da nota.
func (s *Scheduler) markTry(id int64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastTry == nil {
		s.lastTry = map[int64]time.Time{}
	}
	s.lastTry[id] = at
}

// retryEvery devolve o intervalo configurado, ou o padrão de uma hora.
func (s *Scheduler) retryEvery() time.Duration {
	if s.RetryEvery > 0 {
		return s.RetryEvery
	}
	return DefaultRetryEvery
}

// findScheduled acha a nota que o agendamento já gerou no mês.
func findScheduled(invoices []domain.Invoice, scheduleID int64, period string) (domain.Invoice, bool) {
	for _, invoice := range invoices {
		if invoice.ScheduleID == scheduleID && invoice.SchedulePeriod == period {
			return invoice, true
		}
	}
	return domain.Invoice{}, false
}

// sameDay compara as datas no fuso de Brasília.
func sameDay(a, b time.Time, location *time.Location) bool {
	return a.In(location).Format("2006-01-02") == b.In(location).Format("2006-01-02")
}
