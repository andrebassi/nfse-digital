package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestParseEmailList: vírgula e ponto e vírgula separam; espaços e itens
// vazios somem; endereço inválido ou com nome é recusado dizendo qual.
func TestParseEmailList(t *testing.T) {
	list, err := ParseEmailList(" a@b.com, c@d.com.br ;; ")
	if err != nil || strings.Join(list, "|") != "a@b.com|c@d.com.br" {
		t.Fatalf("lista: %v, %v", list, err)
	}
	if list, err := ParseEmailList(""); err != nil || len(list) != 0 {
		t.Fatalf("vazia: %v, %v", list, err)
	}
	for _, bad := range []string{"sem-arroba", "Fulano <a@b.com>", "a@b.com, x y"} {
		if _, err := ParseEmailList(bad); err == nil || !strings.Contains(err.Error(), "e-mail inválido") {
			t.Errorf("%q passou: %v", bad, err)
		}
	}
	if !ValidEmail("a@b.com") || ValidEmail("a@b.com, c@d.com") || ValidEmail("x") || ValidEmail("") {
		t.Error("ValidEmail errado")
	}
}

// TestCustomerEmailValidate: o padrão de um tomador com e-mail passa; sem
// Para, Cópia inválida, assunto e mensagem vazios voltam por campo.
func TestCustomerEmailValidate(t *testing.T) {
	settings := DefaultCustomerEmail(Customer{ID: 7, Email: "nf@cliente.example"})
	if settings.CustomerID != 7 || !settings.AttachPDF || !settings.AttachXML || settings.AutoSend || settings.Validate() != nil {
		t.Fatalf("padrão: %+v, %v", settings, settings.Validate())
	}
	var errs FieldErrors
	err := CustomerEmail{CC: "x"}.Validate()
	if !errors.As(err, &errs) || errs["to"] == "" || errs["cc"] == "" || errs["subject"] == "" || errs["body"] == "" {
		t.Fatalf("vazio: %v", err)
	}
	if err := (CustomerEmail{To: "ruim", Subject: "s", Body: "b"}).Validate(); !errors.As(err, &errs) || !strings.Contains(errs["to"], "ruim") {
		t.Fatalf("Para inválido: %v", err)
	}
}

// TestEmailSenderConfigured: só endereço com senha selada envia.
func TestEmailSenderConfigured(t *testing.T) {
	if (EmailSender{Address: "a@b.com"}).Configured() || (EmailSender{SealedPassword: []byte{1}}).Configured() ||
		!(EmailSender{Address: "a@b.com", SealedPassword: []byte{1}}).Configured() {
		t.Fatal("Configured errado")
	}
}

// TestRenderEmailTemplate: todos os marcadores são trocados pelo valor da
// nota (mês por extenso, R$ com milhar, link de consulta pela chave); o
// desconhecido fica visível; nenhum marcador da lista sobra sem tradução.
func TestRenderEmailTemplate(t *testing.T) {
	issuer := Issuer{LegalName: "EMITENTE LTDA", CNPJ: "11222333000181", CityCode: "3505708"}
	customer := Customer{ID: 3, Name: "CLIENTE LTDA"}
	invoice := Invoice{
		NFSeNumber: "42", Amount: 150000, Competence: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		IssuedAt: time.Date(2026, 11, 2, 18, 0, 0, 0, time.UTC), Description: "Suporte", AccessKey: "123",
	}
	got := RenderEmailTemplate(strings.Join(EmailPlaceholders, "|")+"|{outro}", InvoiceEmailValues(issuer, customer, invoice))
	want := "42|R$ 1.500,00|11/2026|novembro/2026|02/11/2026|CLIENTE LTDA|EMITENTE LTDA|11.222.333/0001-81|Suporte|123|https://www.nfse.gov.br/ConsultaPublica/?tpc=1&chave=123|{outro}"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	// Nota sem competência e sem data de emissão não quebra: os campos ficam vazios.
	if empty := InvoiceEmailValues(issuer, customer, Invoice{}); empty.Competence != "" || empty.IssuedAt != "" || empty.CompetenceMonth != "" {
		t.Fatalf("vazia: %+v", empty)
	}
}

// TestSampleInvoice: a nota de exemplo é a 123 de R$ 1.500,00 na competência
// do mês, com chave de 50 dígitos que começa pelo município e CNPJ.
func TestSampleInvoice(t *testing.T) {
	now := time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)
	issuer := Issuer{CNPJ: "11222333000181", CityCode: "3505708"}
	sample := SampleInvoice(issuer, Customer{ID: 9}, now)
	if sample.NFSeNumber != "123" || sample.Amount != 150000 || sample.Competence.Day() != 1 || sample.CustomerID != 9 ||
		len(sample.AccessKey) != 50 || !strings.HasPrefix(sample.AccessKey, "3505708211222333000181") {
		t.Fatalf("exemplo: %+v", sample)
	}
	// Emitente sem cadastro ainda gera chave de 50 dígitos (completada com zeros).
	if key := SampleInvoice(Issuer{}, Customer{}, now).AccessKey; len(key) != 50 {
		t.Fatalf("chave curta: %q", key)
	}
}
