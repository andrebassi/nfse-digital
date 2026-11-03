package domain

import (
	"fmt"
	"slices"
)

// InvoiceStatus é o estado da nota no ciclo de emissão.
type InvoiceStatus string

const (
	// StatusPending é a nota reservada e enviada, sem resposta definitiva da SEFIN.
	StatusPending InvoiceStatus = "pending"
	// StatusAuthorized é a nota aceita pela SEFIN, com chave de acesso.
	StatusAuthorized InvoiceStatus = "authorized"
	// StatusRejected é a DPS recusada pela SEFIN, com código de erro.
	StatusRejected InvoiceStatus = "rejected"
	// StatusCancelled é a nota autorizada que depois recebeu evento de cancelamento.
	StatusCancelled InvoiceStatus = "cancelled"
)

// allowedTransitions lista as únicas mudanças de estado possíveis. Rejeitada e
// cancelada são finais: corrigir é emitir outra nota, nunca reabrir esta.
var allowedTransitions = map[InvoiceStatus][]InvoiceStatus{
	StatusPending:    {StatusAuthorized, StatusRejected},
	StatusAuthorized: {StatusCancelled},
}

// TransitionTo devolve o novo estado ou erro quando a mudança não é permitida.
func (s InvoiceStatus) TransitionTo(next InvoiceStatus) (InvoiceStatus, error) {
	if slices.Contains(allowedTransitions[s], next) {
		return next, nil
	}
	return s, fmt.Errorf("transição de %q para %q não é permitida", s, next)
}

// Label devolve o estado como aparece na tela.
func (s InvoiceStatus) Label() string {
	switch s {
	case StatusPending:
		return "pendente"
	case StatusAuthorized:
		return "autorizada"
	case StatusRejected:
		return "rejeitada"
	case StatusCancelled:
		return "cancelada"
	default:
		return string(s)
	}
}
