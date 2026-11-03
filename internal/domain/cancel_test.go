package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// cancelFixture é uma nota autorizada fictícia e o emitente dela.
func cancelFixture() (Issuer, Invoice) {
	key := "35057082112223330001810000000000003826100000000001"
	return Issuer{CNPJ: "11.222.333/0001-81"}, Invoice{Status: StatusAuthorized, AccessKey: key, Environment: EnvHomologation}
}

// TestBuildCancelEvent: Id no formato do XSD (59 posições), motivo traduzido,
// justificativa aparada e verAplic cortado em 20.
func TestBuildCancelEvent(t *testing.T) {
	issuer, invoice := cancelFixture()
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	event, err := BuildCancelEvent(issuer, invoice, "servico_nao_prestado", "  serviço não foi prestado  ", strings.Repeat("v", 30), at)
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "PRE"+invoice.AccessKey+"101101" || len(event.ID) != 59 {
		t.Fatalf("Id: %q (%d)", event.ID, len(event.ID))
	}
	if event.ReasonCode != "2" || event.Description != "serviço não foi prestado" || event.AuthorCNPJ != "11222333000181" {
		t.Fatalf("evento: %+v", event)
	}
	if len(event.AppVersion) != 20 || !event.At.Equal(at) || event.Environment != EnvHomologation {
		t.Fatalf("verAplic/data/ambiente: %+v", event)
	}
	for reason, code := range map[string]string{"erro_emissao": "1", "outros": "9"} {
		if e, _ := BuildCancelEvent(issuer, invoice, reason, strings.Repeat("x", 15), "v", at); e.ReasonCode != code {
			t.Errorf("%s → %q", reason, e.ReasonCode)
		}
	}
}

// TestBuildCancelEventRefuses: nota não autorizada, motivo e justificativa
// inválidos e emitente sem CNPJ voltam como erro antes de ir à SEFIN.
func TestBuildCancelEventRefuses(t *testing.T) {
	issuer, invoice := cancelFixture()
	for _, status := range []InvoiceStatus{StatusPending, StatusRejected, StatusCancelled} {
		other := invoice
		other.Status = status
		if _, err := BuildCancelEvent(issuer, other, "outros", strings.Repeat("x", 15), "v", time.Now()); !errors.Is(err, ErrNotCancellable) {
			t.Errorf("%s: %v", status, err)
		}
	}
	noKey := invoice
	noKey.AccessKey = ""
	if _, err := BuildCancelEvent(issuer, noKey, "outros", strings.Repeat("x", 15), "v", time.Now()); !errors.Is(err, ErrNotCancellable) {
		t.Errorf("sem chave: %v", err)
	}

	badKey := invoice
	badKey.AccessKey = "123"
	_, err := BuildCancelEvent(Issuer{CNPJ: "529.982.247-25"}, badKey, "talvez", strings.Repeat("x", 14), "v", time.Now())
	fields, ok := err.(FieldErrors)
	if !ok || len(fields) != 4 || fields["reason"] == "" || fields["description"] == "" || fields["issuer"] == "" || fields["access_key"] == "" {
		t.Fatalf("erros: %v", err)
	}
	if _, err := BuildCancelEvent(issuer, invoice, "outros", strings.Repeat("x", 256), "v", time.Now()); err == nil {
		t.Fatal("justificativa acima de 255 deveria falhar")
	}
}
