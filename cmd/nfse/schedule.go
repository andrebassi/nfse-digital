package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"nfse/internal/adapters/ntfy"
	"nfse/internal/adapters/sefin"
	"nfse/internal/adapters/sqlite"
	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// scheduleUsage lista os subcomandos de agendamento.
const scheduleUsage = "uso: nfse schedule <add|list|pause|resume|remove|run> [opções]"

// scheduleCommand despacha os subcomandos de agendamento.
func scheduleCommand(args []string) error {
	if len(args) == 0 {
		return errors.New(scheduleUsage)
	}
	switch args[0] {
	case "add":
		return scheduleAdd(args[1:])
	case "list":
		return scheduleList(args[1:])
	case "pause", "resume", "remove":
		return scheduleChange(args[0], args[1:])
	case "run":
		return scheduleRun(args[1:])
	}
	return errors.New(scheduleUsage)
}

// scheduleAdd cria um agendamento mensal. A permissão de produção é gravada
// aqui, na criação, e não pode ser dada depois por quem só roda o agendador.
func scheduleAdd(args []string) error {
	flags := flag.NewFlagSet("schedule add", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	customerDoc := flags.String("customer", "", "CPF/CNPJ do tomador")
	templateName := flags.String("template", "", "nome do modelo de serviço")
	amount := flags.String("amount", "", "valor, como 19000.00 (padrão: o do modelo)")
	day := flags.Int("day", 0, "dia do mês, de 1 a 31 (29–31 caem no último dia em mês curto)")
	competence := flags.String("competence", string(domain.CompetencePreviousMonth), "emission ou previous-month")
	description := flags.String("description", "", "descrição com {mes}, {ano}, {competencia} (padrão: a do modelo)")
	notifyEve := flags.Bool("notify-eve", true, "avisa na véspera às 09:00 pelo ntfy")
	production := flags.Bool("production", false, "permite emitir com valor fiscal quando o emitente estiver em produção")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	ctx := context.Background()
	customer, template, err := resolveCustomerTemplate(ctx, repo, *customerDoc, *templateName)
	if err != nil {
		return err
	}
	value := template.DefaultAmount
	if *amount != "" {
		if value, err = domain.ParseBRL(*amount); err != nil {
			return fmt.Errorf("--amount: %w", err)
		}
	}
	schedule := domain.Schedule{
		TemplateID: template.ID, CustomerID: customer.ID, Amount: value, Day: *day, Competence: domain.CompetenceMode(*competence),
		Description: *description, NotifyEve: *notifyEve, Active: true, Production: *production,
	}
	if err := schedule.Validate(); err != nil {
		return fmt.Errorf("dados inválidos: %w", err)
	}
	if err := repo.SaveSchedule(ctx, &schedule); err != nil {
		return err
	}
	fmt.Printf("agendamento #%d criado\n", schedule.ID)
	return printSchedule(schedule, customer, template)
}

// resolveCustomerTemplate acha o tomador pelo documento e o modelo pelo nome exato.
func resolveCustomerTemplate(ctx context.Context, repo ports.Repository, document, name string) (domain.Customer, domain.Template, error) {
	parsed, err := domain.ParseDocument(document)
	if err != nil {
		return domain.Customer{}, domain.Template{}, fmt.Errorf("--customer: %w", err)
	}
	customers, _ := repo.Customers(ctx, parsed.Digits())
	if len(customers) == 0 {
		return domain.Customer{}, domain.Template{}, fmt.Errorf("tomador %s não cadastrado", parsed)
	}
	templates, _ := repo.Templates(ctx, name)
	for _, template := range templates {
		if template.Name == name {
			return customers[0], template, nil
		}
	}
	return domain.Customer{}, domain.Template{}, fmt.Errorf("modelo %q não cadastrado", name)
}

// scheduleList mostra cada agendamento com as próximas quatro emissões.
func scheduleList(args []string) error {
	flags := flag.NewFlagSet("schedule list", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	ctx := context.Background()
	schedules, err := repo.Schedules(ctx)
	if err != nil {
		return err
	}
	if len(schedules) == 0 {
		fmt.Println("nenhum agendamento")
	}
	for _, schedule := range schedules {
		customer, _ := repo.Customer(ctx, schedule.CustomerID)
		template, _ := repo.Template(ctx, schedule.TemplateID)
		if err := printSchedule(schedule, customer, template); err != nil {
			return err
		}
	}
	return nil
}

// printSchedule imprime o agendamento e as próximas emissões com a descrição resolvida.
func printSchedule(schedule domain.Schedule, customer domain.Customer, template domain.Template) error {
	state := "ativo"
	if !schedule.Active {
		state = "pausado"
	}
	fmt.Printf("#%d · %s · dia %d · %s · %s · %s · %s\n", schedule.ID, state, schedule.Day, schedule.Amount.FormatBRL(),
		customer.Name, template.Name, schedule.Competence)
	description := schedule.Description
	if description == "" {
		description = template.Description
	}
	for _, run := range schedule.NextRuns(time.Now(), 4, domain.SaoPaulo()) {
		fmt.Printf("    %s · aviso %s · competência %s · %s\n", run.EmitAt.Format("02/01/2006 15:04"), run.NotifyAt.Format("02/01 15:04"),
			run.Competence.Format("01/2006"), domain.RenderDescription(description, run.Competence))
	}
	return nil
}

// scheduleChange pausa, retoma ou apaga um agendamento pelo ID.
func scheduleChange(action string, args []string) error {
	flags := flag.NewFlagSet("schedule "+action, flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	if err := flags.Parse(args); err != nil {
		return err
	}
	id, err := strconv.ParseInt(flags.Arg(0), 10, 64)
	if err != nil {
		return fmt.Errorf("uso: nfse schedule %s [--db arquivo] <id>", action)
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	ctx := context.Background()
	if action == "remove" {
		if err := repo.DeleteSchedule(ctx, id); err != nil {
			return err
		}
		fmt.Printf("agendamento #%d apagado (as notas que ele gerou ficam)\n", id)
		return nil
	}
	schedules, err := repo.Schedules(ctx)
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		if schedule.ID == id {
			schedule.Active = action == "resume"
			if err := repo.SaveSchedule(ctx, &schedule); err != nil {
				return err
			}
			fmt.Printf("agendamento #%d %s\n", id, map[bool]string{true: "retomado", false: "pausado"}[schedule.Active])
			return nil
		}
	}
	return ports.ErrNotFound
}

// scheduleRun roda uma rodada do agendador agora, com o emissor real.
func scheduleRun(args []string) error {
	flags := flag.NewFlagSet("schedule run", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	dryRun := flags.Bool("dry-run", false, "só mostra o que sairia, sem emitir nem avisar")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	scheduler := newScheduler(repo, cliAccount)
	outcomes, err := scheduler.Run(context.Background(), time.Now(), *dryRun)
	if err != nil {
		return err
	}
	if len(outcomes) == 0 {
		fmt.Println("nada a fazer agora")
	}
	for _, outcome := range outcomes {
		printOutcome(outcome)
	}
	return nil
}

// newScheduler monta o agendador com o emissor real da SEFIN e, se a variável
// NFSE_NTFY_URL estiver definida, o aviso pelo ntfy.
func newScheduler(repo *sqlite.Repository, accountID int64) *app.Scheduler {
	// AllowProduction fica ligado aqui porque a permissão de produção do
	// agendador é por agendamento (gravada no `schedule add --production`) e
	// é conferida pela guarda do app antes de cada emissão.
	emitter := &sefin.Emitter{Certificates: certificatesFor(accountID), AppVersion: "nfse-digital " + version, AllowProduction: true}
	service := &app.Service{Repo: repo, Emitter: emitter, Now: time.Now, Location: domain.SaoPaulo()}
	// Nota agendada também sai por e-mail quando o tomador pediu envio
	// automático. Sem a chave do cofre, o agendador segue sem e-mail.
	if err := withEmail(service, accountID); err != nil {
		log.Printf("agendador sem envio por e-mail: %v", err)
	}
	scheduler := &app.Scheduler{Service: service}
	if url := os.Getenv("NFSE_NTFY_URL"); url != "" {
		scheduler.Notifier = ntfy.New(url)
	}
	return scheduler
}

// printOutcome descreve em português o que a rodada fez com o agendamento.
func printOutcome(outcome app.ScheduleOutcome) {
	labels := map[string]string{
		app.ActionEmitted: "emitida", app.ActionRetried: "reenviada", app.ActionWouldEmit: "sairia agora",
		app.ActionBlockedProduction: "bloqueada: emitente em produção e o agendamento não tem --production",
		app.ActionAlreadyEmitted:    "já emitida por outro processo", app.ActionNotified: "aviso de véspera enviado",
		app.ActionWouldNotify: "aviso de véspera sairia agora", app.ActionNotifyFailed: "aviso de véspera falhou",
	}
	line := fmt.Sprintf("agendamento #%d · %s · %s", outcome.ScheduleID, outcome.Period, labels[outcome.Action])
	if outcome.Invoice.ID > 0 {
		line += fmt.Sprintf(" · nota #%d %s", outcome.Invoice.ID, outcome.Invoice.Status.Label())
	}
	if outcome.Err != nil {
		line += " · " + outcome.Err.Error()
	}
	fmt.Println(line)
}
