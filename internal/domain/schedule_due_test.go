package domain

import (
	"testing"
	"time"
)

// TestDueRun: antes das 03:00 do dia não vence; a partir daí vence até o fim
// do mês; dia 31 em fevereiro vence no dia 28; pausado nunca vence.
func TestDueRun(t *testing.T) {
	schedule := Schedule{Day: 28, Competence: CompetencePreviousMonth, Active: true}
	cases := []struct {
		now  time.Time
		due  bool
		emit string
	}{
		{time.Date(2026, 11, 28, 2, 59, 0, 0, brt), false, ""},
		{time.Date(2026, 11, 28, 3, 0, 0, 0, brt), true, "2026-11-28 03:00"},
		{time.Date(2026, 11, 30, 23, 0, 0, 0, brt), true, "2026-11-28 03:00"},
		{time.Date(2026, 12, 1, 0, 0, 0, 0, brt), false, ""},
	}
	for _, c := range cases {
		run, due := schedule.DueRun(c.now, brt)
		if due != c.due || (due && run.EmitAt.Format("2006-01-02 15:04") != c.emit) {
			t.Errorf("%s: due=%v run=%v", c.now, due, run.EmitAt)
		}
	}
	run, _ := schedule.DueRun(time.Date(2026, 11, 28, 3, 0, 0, 0, brt), brt)
	if PeriodKey(run.EmitAt) != "2026-11" || PeriodKey(run.Competence) != "2026-10" {
		t.Errorf("período %s, competência %s", PeriodKey(run.EmitAt), PeriodKey(run.Competence))
	}
	february := Schedule{Day: 31, Competence: CompetenceEmission, Active: true}
	if run, due := february.DueRun(time.Date(2027, 2, 28, 3, 0, 0, 0, brt), brt); !due || run.EmitAt.Day() != 28 {
		t.Errorf("fevereiro: %v %v", due, run.EmitAt)
	}
	if _, due := (Schedule{Day: 1}).DueRun(time.Date(2026, 11, 2, 0, 0, 0, 0, brt), brt); due {
		t.Error("pausado venceu")
	}
}

// TestEveRun: aviso só a partir das 09:00 da véspera, só com aviso ligado e
// agendamento ativo.
func TestEveRun(t *testing.T) {
	schedule := Schedule{Day: 28, Competence: CompetenceEmission, Active: true, NotifyEve: true}
	if _, ok := schedule.EveRun(time.Date(2026, 11, 27, 8, 59, 0, 0, brt), brt); ok {
		t.Error("avisou antes das 09:00")
	}
	run, ok := schedule.EveRun(time.Date(2026, 11, 27, 9, 0, 0, 0, brt), brt)
	if !ok || run.EmitAt.Format("2006-01-02") != "2026-11-28" {
		t.Errorf("véspera: %v %v", ok, run.EmitAt)
	}
	silent := schedule
	silent.NotifyEve = false
	paused := schedule
	paused.Active = false
	for _, s := range []Schedule{silent, paused} {
		if _, ok := s.EveRun(time.Date(2026, 11, 27, 10, 0, 0, 0, brt), brt); ok {
			t.Errorf("avisou: %+v", s)
		}
	}
}
