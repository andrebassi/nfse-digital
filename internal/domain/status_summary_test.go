package domain

import (
	"testing"
	"time"
)

// TestTransitions percorre todos os pares de estado: só os quatro previstos passam.
func TestTransitions(t *testing.T) {
	all := []InvoiceStatus{StatusPending, StatusAuthorized, StatusRejected, StatusCancelled}
	allowed := map[[2]InvoiceStatus]bool{
		{StatusPending, StatusAuthorized}:   true,
		{StatusPending, StatusRejected}:     true,
		{StatusAuthorized, StatusCancelled}: true,
	}
	for _, from := range all {
		for _, to := range all {
			got, err := from.TransitionTo(to)
			if allowed[[2]InvoiceStatus{from, to}] {
				if err != nil || got != to {
					t.Errorf("%s→%s deveria passar: %v", from, to, err)
				}
			} else if err == nil || got != from {
				t.Errorf("%s→%s deveria falhar e manter o estado", from, to)
			}
		}
	}
}

// TestStatusLabel confere os rótulos em português e o fallback.
func TestStatusLabel(t *testing.T) {
	want := map[InvoiceStatus]string{StatusPending: "pendente", StatusAuthorized: "autorizada", StatusRejected: "rejeitada", StatusCancelled: "cancelada", "outro": "outro"}
	for status, label := range want {
		if status.Label() != label {
			t.Errorf("%s: %q", status, status.Label())
		}
	}
}

// TestParsePeriod aceita as quatro janelas (365 para o histórico importado
// do portal) e cai no padrão no resto.
func TestParsePeriod(t *testing.T) {
	cases := map[string]Period{"7": 7, "30": 30, "90": 90, "365": 365, "": 30, "15": 30, "abc": 30}
	for input, want := range cases {
		if got := ParsePeriod(input); got != want {
			t.Errorf("ParsePeriod(%q) = %d", input, got)
		}
	}
	if len(Periods()) != 4 {
		t.Error("deveria haver quatro janelas")
	}
}

// sampleInvoices monta notas em todos os estados e datas diferentes.
func sampleInvoices(now time.Time) []Invoice {
	return []Invoice{
		{ID: 1, Status: StatusAuthorized, Amount: 2288500, IssuedAt: now.AddDate(0, 0, -5)},
		{ID: 2, Status: StatusAuthorized, Amount: 1900000, IssuedAt: now.AddDate(0, 0, -20)},
		{ID: 3, Status: StatusRejected, Amount: 1900000, IssuedAt: now.AddDate(0, 0, -1)},
		{ID: 4, Status: StatusCancelled, Amount: 1000000, IssuedAt: now.AddDate(0, 0, -40)},
		{ID: 5, Status: StatusPending, Amount: 500000, IssuedAt: now.AddDate(0, 0, -2)},
		{ID: 6, Status: StatusAuthorized, Amount: 100, IssuedAt: now.AddDate(0, 0, 1)},
	}
}

// TestFilterByPeriod exclui o que está fora da janela e o que está no futuro.
func TestFilterByPeriod(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	if got := len(FilterByPeriod(sampleInvoices(now), 7, now)); got != 3 {
		t.Errorf("7 dias: %d notas", got)
	}
	if got := len(FilterByPeriod(sampleInvoices(now), 90, now)); got != 5 {
		t.Errorf("90 dias: %d notas", got)
	}
}

// TestFilterByStatus filtra por estado e devolve tudo com estado vazio.
func TestFilterByStatus(t *testing.T) {
	invoices := sampleInvoices(time.Now())
	if len(FilterByStatus(invoices, "")) != 6 || len(FilterByStatus(invoices, StatusAuthorized)) != 3 {
		t.Fatal("filtro por estado errado")
	}
}

// TestSummarizeCountsOnlyAuthorizedRevenue é a invariante: receita e ticket
// médio nunca incluem nota rejeitada, cancelada ou pendente.
func TestSummarizeCountsOnlyAuthorizedRevenue(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	summary := Summarize(FilterByPeriod(sampleInvoices(now), 90, now))
	if summary.Total != 5 || summary.Authorized != 2 || summary.Rejected != 1 || summary.Cancelled != 1 || summary.Pending != 1 {
		t.Fatalf("contagens erradas: %+v", summary)
	}
	if summary.Revenue != 4188500 || summary.AverageTicket != 2094250 {
		t.Fatalf("receita %d ticket %d", summary.Revenue, summary.AverageTicket)
	}
	if empty := Summarize(nil); empty.AverageTicket != 0 || empty.Revenue != 0 {
		t.Fatal("sem nota autorizada o ticket médio é zero")
	}
}

// TestTranslateSefinError traduz o E0039 e devolve cru o código desconhecido.
func TestTranslateSefinError(t *testing.T) {
	if got := TranslateSefinError("E0039", "original"); got == "original" {
		t.Error("E0039 não foi traduzido")
	}
	if got := TranslateSefinError("E9999", "original"); got != "original" {
		t.Errorf("código desconhecido: %q", got)
	}
}
