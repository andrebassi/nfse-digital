package domain

import (
	"errors"
	"strings"
	"time"
)

// CancelEventType é o código do evento de cancelamento no XSD (e101101).
const CancelEventType = "101101"

// cancelDescriptionMin e cancelDescriptionMax são os limites do xMotivo no XSD
// (TSMotivo): menos de 15 a SEFIN recusa, mais de 255 também.
const (
	cancelDescriptionMin = 15
	cancelDescriptionMax = 255
)

// cancelReasonCodes traduz o motivo da linha de comando para o cMotivo do XSD
// (TSCodJustCanc): 1 erro na emissão, 2 serviço não prestado, 9 outros.
var cancelReasonCodes = map[string]string{"erro_emissao": "1", "servico_nao_prestado": "2", "outros": "9"} // contract:ok — valores aceitos na flag --reason, documentados na story 3.2

// ErrNotCancellable recusa cancelar nota que não está autorizada: rascunho,
// rejeitada e já cancelada não têm o que anular na SEFIN.
var ErrNotCancellable = errors.New("só nota autorizada pode ser cancelada")

// CancelEvent é o pedido de registro do evento de cancelamento, já com os
// códigos da SEFIN. Não sabe nada de XML nem de assinatura.
type CancelEvent struct {
	ID          string
	Environment Environment
	AppVersion  string
	At          time.Time
	AuthorCNPJ  string
	AccessKey   string
	ReasonCode  string
	Description string
}

// BuildCancelEvent valida o pedido e monta o evento. O Id segue o padrão do
// TSIdPedRegEvt: "PRE" + chave (50) + tipo do evento (6), 59 posições. A
// documentação do XSD cita também o número do pedido, mas o regex dele não
// comporta: vale o regex, que é o que a SEFIN valida.
func BuildCancelEvent(issuer Issuer, invoice Invoice, reason, description, appVersion string, at time.Time) (CancelEvent, error) {
	if invoice.Status != StatusAuthorized || invoice.AccessKey == "" {
		return CancelEvent{}, ErrNotCancellable
	}
	errs := FieldErrors{}
	code, ok := cancelReasonCodes[reason]
	if !ok {
		errs["reason"] = "motivo inválido: use erro_emissao, servico_nao_prestado ou outros"
	}
	description = strings.TrimSpace(description)
	if size := len([]rune(description)); size < cancelDescriptionMin || size > cancelDescriptionMax {
		errs["description"] = "a justificativa precisa ter de 15 a 255 caracteres"
	}
	document, err := ParseDocument(issuer.CNPJ)
	if err != nil || document.Kind() != KindCNPJ {
		errs["issuer"] = "o autor do cancelamento precisa ser o CNPJ do emitente"
	}
	if !isDigits(invoice.AccessKey, 50) {
		errs["access_key"] = "chave de acesso da nota inválida"
	}
	if err := errs.orNil(); err != nil {
		return CancelEvent{}, err
	}
	return CancelEvent{
		ID:          "PRE" + invoice.AccessKey + CancelEventType,
		Environment: invoice.Environment,
		AppVersion:  truncateRunes(appVersion, maxAppVersionLength),
		At:          at,
		AuthorCNPJ:  document.Digits(),
		AccessKey:   invoice.AccessKey,
		ReasonCode:  code,
		Description: description,
	}, nil
}
