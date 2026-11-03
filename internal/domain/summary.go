package domain

import (
	"strconv"
	"time"
)

// Period é a janela de dias da lista de notas.
type Period int

// periods são as janelas oferecidas na tela; o padrão é 30 dias. 365 existe
// para a importação do portal, que traz o histórico do ano de uma vez.
var periods = []Period{7, 30, 90, 365}

// DefaultPeriod é a janela usada quando a tela não informa nenhuma válida.
const DefaultPeriod Period = 30

// Periods devolve as janelas oferecidas, na ordem da tela.
func Periods() []Period { return append([]Period(nil), periods...) }

// ParsePeriod lê a janela vinda da URL; valor fora da lista cai no padrão em
// vez de dar erro, porque é só um filtro de tela.
func ParsePeriod(text string) Period {
	for _, period := range periods {
		if text == strconv.Itoa(int(period)) {
			return period
		}
	}
	return DefaultPeriod
}

// FilterByPeriod devolve as notas emitidas dentro da janela que termina em `now`.
func FilterByPeriod(invoices []Invoice, period Period, now time.Time) []Invoice {
	since := now.AddDate(0, 0, -int(period))
	result := []Invoice{}
	for _, invoice := range invoices {
		if !invoice.IssuedAt.Before(since) && !invoice.IssuedAt.After(now) {
			result = append(result, invoice)
		}
	}
	return result
}

// FilterByStatus devolve as notas num estado; estado vazio devolve todas.
func FilterByStatus(invoices []Invoice, status InvoiceStatus) []Invoice {
	if status == "" {
		return invoices
	}
	result := []Invoice{}
	for _, invoice := range invoices {
		if invoice.Status == status {
			result = append(result, invoice)
		}
	}
	return result
}

// Summary são os indicadores do topo da tela de notas.
type Summary struct {
	Total         int
	Authorized    int
	Cancelled     int
	Rejected      int
	Pending       int
	Revenue       Money
	AverageTicket Money
}

// Summarize calcula os indicadores. Receita e ticket médio contam só nota
// autorizada: rejeitada e cancelada não viram faturamento.
func Summarize(invoices []Invoice) Summary {
	summary := Summary{Total: len(invoices)}
	for _, invoice := range invoices {
		switch invoice.Status {
		case StatusAuthorized:
			summary.Authorized++
			summary.Revenue += invoice.Amount
		case StatusCancelled:
			summary.Cancelled++
		case StatusRejected:
			summary.Rejected++
		case StatusPending:
			summary.Pending++
		}
	}
	if summary.Authorized > 0 {
		summary.AverageTicket = summary.Revenue / Money(summary.Authorized)
	}
	return summary
}
