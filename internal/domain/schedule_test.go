package domain

import (
	"errors"
	"testing"
	"time"
)

// brt é UTC-3 fixo, para o teste não depender do tzdata da máquina.
var brt = time.FixedZone("BRT", -3*60*60)

// TestNextRunsMatchesReference reproduz as datas medidas no SaaS usado antes em
// 29/10/2026 para o dia 28: 28/11, 28/12, 28/01 e 28/02 às 03:00, aviso na véspera às 09:00.
func TestNextRunsMatchesReference(t *testing.T) {
	now := time.Date(2026, 10, 29, 22, 52, 0, 0, brt)
	runs := Schedule{Day: 28, Competence: CompetenceEmission}.NextRuns(now, 4, brt)
	want := []string{"2026-11-28 03:00", "2026-12-28 03:00", "2027-01-28 03:00", "2027-02-28 03:00"}
	for i, run := range runs {
		if got := run.EmitAt.Format("2006-01-02 15:04"); got != want[i] {
			t.Errorf("emissão %d = %s; want %s", i, got, want[i])
		}
	}
	if got := runs[0].NotifyAt.Format("2006-01-02 15:04"); got != "2026-11-27 09:00" {
		t.Errorf("aviso = %s", got)
	}
	if got := runs[0].Competence.Format("2006-01-02"); got != "2026-11-28" {
		t.Errorf("competência = %s", got)
	}
}

// TestNextRunsPreviousMonthCompetence: emissão de 28/01/2027 se refere a 12/2026.
func TestNextRunsPreviousMonthCompetence(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, brt)
	runs := Schedule{Day: 28, Competence: CompetencePreviousMonth}.NextRuns(now, 3, brt)
	if got := runs[2].Competence.Format("01/2006"); got != "12/2026" {
		t.Fatalf("competência = %s", got)
	}
}

// TestNextRunsIncludesTodayWhenStillAhead: dia 30 às 01:00 ainda pega a emissão das 03:00.
func TestNextRunsIncludesTodayWhenStillAhead(t *testing.T) {
	now := time.Date(2026, 10, 30, 1, 0, 0, 0, brt)
	runs := Schedule{Day: 30, Competence: CompetenceEmission}.NextRuns(now, 1, brt)
	if got := runs[0].EmitAt.Format("2006-01-02"); got != "2026-10-30" {
		t.Fatalf("primeira emissão = %s", got)
	}
}

// TestNextRunsLastDayOfShortMonth: dia 31 cai em 30/11 e 28/02, e nenhum mês fica sem nota.
func TestNextRunsLastDayOfShortMonth(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, brt)
	runs := Schedule{Day: 31, Competence: CompetenceEmission}.NextRuns(now, 4, brt)
	want := []string{"2026-11-30", "2026-12-31", "2027-01-31", "2027-02-28"}
	for i, run := range runs {
		if got := run.EmitAt.Format("2006-01-02"); got != want[i] {
			t.Errorf("emissão %d = %s; want %s", i, got, want[i])
		}
	}
}

// TestScheduleValidate aceita agendamento completo e lista cada campo faltando.
func TestScheduleValidate(t *testing.T) {
	ok := Schedule{TemplateID: 1, CustomerID: 1, Amount: 1900000, Day: 28, Competence: CompetencePreviousMonth}
	if err := ok.Validate(); err != nil {
		t.Fatalf("agendamento válido recusado: %v", err)
	}
	fields := (Schedule{Day: 32}).Validate().(FieldErrors)
	for _, field := range []string{"template_id", "customer_id", "amount", "day", "competence"} {
		if fields[field] == "" {
			t.Errorf("faltou erro em %s", field)
		}
	}
}

// TestRenderDescription troca as três variáveis pelo mês da competência.
func TestRenderDescription(t *testing.T) {
	competence := time.Date(2026, 11, 28, 0, 0, 0, 0, brt)
	got := RenderDescription("referente a {mes}/{ano} ({competencia})", competence)
	if got != "referente a novembro/2026 (11/2026)" {
		t.Fatalf("got %q", got)
	}
}

// TestSaoPauloFallback simula sistema sem tzdata: o fuso cai para UTC-3 fixo.
func TestSaoPauloFallback(t *testing.T) {
	original := loadLocation
	defer func() { loadLocation = original }()
	loadLocation = func(string) (*time.Location, error) { return nil, errors.New("sem tzdata") }
	_, offset := time.Date(2026, 1, 1, 0, 0, 0, 0, SaoPaulo()).Zone()
	if offset != -3*60*60 {
		t.Fatalf("offset = %d", offset)
	}
	loadLocation = func(string) (*time.Location, error) { return time.UTC, nil }
	if SaoPaulo() != time.UTC {
		t.Fatal("deveria usar o fuso carregado")
	}
}
