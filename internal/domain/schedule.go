package domain

import (
	"strconv"
	"strings"
	"time"
)

// CompetenceMode diz a que mês a nota agendada se refere.
type CompetenceMode string

const (
	// CompetenceEmission usa a própria data da emissão (padrão do SaaS usado antes).
	CompetenceEmission CompetenceMode = "emission"
	// CompetencePreviousMonth usa o mês anterior: emitir em 28/11 sobre outubro.
	CompetencePreviousMonth CompetenceMode = "previous-month"
)

// Horários fixos do agendamento, em Brasília: a emissão sai de madrugada para
// não competir com uso manual, e o aviso chega na véspera em horário comercial.
const (
	emitHour   = 3
	notifyHour = 9
)

// Schedule é uma nota recorrente: todo mês, no dia escolhido, com o valor padrão.
type Schedule struct {
	ID             int64
	TemplateID     int64
	CustomerID     int64
	Amount         Money
	Day            int
	Competence     CompetenceMode
	Description    string
	NotifyEve      bool
	SendToCustomer bool
	Active         bool
	// Production grava, na criação, que o dono aceitou emitir com valor
	// fiscal por este agendamento; sem ela, emitente em produção não emite.
	Production bool
	// NotifiedPeriod é o último mês (AAAA-MM) cujo aviso de véspera saiu.
	NotifiedPeriod string
}

// ScheduledRun é uma emissão futura calculada: quando sai, quando avisa e de que competência.
type ScheduledRun struct {
	EmitAt     time.Time
	NotifyAt   time.Time
	Competence time.Time
}

// loadLocation é variável de pacote para o teste simular sistema sem tzdata.
var loadLocation = time.LoadLocation

// SaoPaulo devolve o fuso de Brasília. Sem tzdata no sistema, cai para UTC-3
// fixo, que é exato desde 2019, quando o Brasil deixou o horário de verão.
func SaoPaulo() *time.Location {
	location, err := loadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("BRT", -3*60*60)
	}
	return location
}

// Validate confere o agendamento antes de gravar.
func (s Schedule) Validate() error {
	errs := FieldErrors{}
	if s.TemplateID <= 0 {
		errs["template_id"] = "escolha o modelo de serviço"
	}
	if s.CustomerID <= 0 {
		errs["customer_id"] = "escolha o tomador"
	}
	if s.Amount <= 0 {
		errs["amount"] = "o valor precisa ser maior que zero"
	}
	if s.Day < 1 || s.Day > 31 {
		errs["day"] = "o dia vai de 1 a 31"
	}
	if s.Competence != CompetenceEmission && s.Competence != CompetencePreviousMonth {
		errs["competence"] = "escolha a competência"
	}
	return errs.orNil()
}

// NextRuns calcula as próximas `count` emissões depois de `now`. Dia 29 a 31
// em mês mais curto cai no último dia do mês, para nenhum mês ficar sem nota.
func (s Schedule) NextRuns(now time.Time, count int, location *time.Location) []ScheduledRun {
	local := now.In(location)
	year, month := local.Year(), local.Month()
	runs := make([]ScheduledRun, 0, count)
	for len(runs) < count {
		emitAt := time.Date(year, month, effectiveDay(year, month, s.Day), emitHour, 0, 0, 0, location)
		if emitAt.After(local) {
			eve := emitAt.AddDate(0, 0, -1)
			runs = append(runs, ScheduledRun{
				EmitAt:     emitAt,
				NotifyAt:   time.Date(eve.Year(), eve.Month(), eve.Day(), notifyHour, 0, 0, 0, location),
				Competence: s.competenceFor(emitAt),
			})
		}
		// Avança um mês pelo dia 1, para o AddDate não pular fevereiro.
		next := time.Date(year, month, 1, 0, 0, 0, 0, location).AddDate(0, 1, 0)
		year, month = next.Year(), next.Month()
	}
	return runs
}

// PeriodKey é o mês (AAAA-MM) de uma emissão, a chave que impede duas notas do
// mesmo agendamento no mesmo mês.
func PeriodKey(t time.Time) string {
	return t.Format("2006-01")
}

// DueRun diz se a emissão deste mês já venceu em `now`: do dia efetivo às
// 03:00 até o fim do mês. Depois disso o mês passou e não é mais emitido
// sozinho — nota atrasada de um mês inteiro pede decisão do dono.
func (s Schedule) DueRun(now time.Time, location *time.Location) (ScheduledRun, bool) {
	if !s.Active {
		return ScheduledRun{}, false
	}
	local := now.In(location)
	emitAt := time.Date(local.Year(), local.Month(), effectiveDay(local.Year(), local.Month(), s.Day), emitHour, 0, 0, 0, location)
	if local.Before(emitAt) {
		return ScheduledRun{}, false
	}
	return ScheduledRun{EmitAt: emitAt, Competence: s.competenceFor(emitAt)}, true
}

// EveRun devolve a emissão de amanhã quando já passou das 09:00 da véspera e
// ela ainda não saiu: é a janela do aviso.
func (s Schedule) EveRun(now time.Time, location *time.Location) (ScheduledRun, bool) {
	if !s.Active || !s.NotifyEve {
		return ScheduledRun{}, false
	}
	runs := s.NextRuns(now, 1, location)
	if now.Before(runs[0].NotifyAt) {
		return ScheduledRun{}, false
	}
	return runs[0], true
}

// competenceFor devolve a data de competência de uma emissão conforme o modo.
func (s Schedule) competenceFor(emitAt time.Time) time.Time {
	if s.Competence == CompetencePreviousMonth {
		return time.Date(emitAt.Year(), emitAt.Month(), 1, 0, 0, 0, 0, emitAt.Location()).AddDate(0, -1, 0)
	}
	return time.Date(emitAt.Year(), emitAt.Month(), emitAt.Day(), 0, 0, 0, 0, emitAt.Location())
}

// effectiveDay limita o dia ao último dia do mês.
func effectiveDay(year int, month time.Month, day int) int {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		return last
	}
	return day
}

// monthNames são os meses por extenso, em minúsculas, como entram na descrição.
var monthNames = []string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}

// RenderDescription troca as variáveis da descrição pelo mês da competência:
// {mes} vira "novembro", {ano} vira "2026" e {competencia} vira "11/2026".
func RenderDescription(text string, competence time.Time) string {
	replacer := strings.NewReplacer(
		"{mes}", monthNames[competence.Month()-1],
		"{ano}", strconv.Itoa(competence.Year()),
		"{competencia}", competence.Format("01/2006"),
	)
	return replacer.Replace(text)
}
