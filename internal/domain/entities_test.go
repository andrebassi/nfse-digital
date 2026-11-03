package domain

import (
	"strings"
	"testing"
)

// validIssuer devolve o emitente real, que precisa passar na validação.
func validIssuer() Issuer {
	return Issuer{
		LegalName: "EXEMPLO TECNOLOGIA LTDA", CNPJ: "11222333000181",
		CityCode: "3505708", Environment: EnvHomologation, Series: "1", NextDPSNumber: 1001,
	}
}

// validTemplate devolve o modelo real da Ipê.
func validTemplate() Template {
	return Template{
		Name: "Ipê mensal", Description: "Referente a prestação de serviço", NationalTaxCode: "010701",
		MunicipalTaxCode: "220", NBS: "115013000", ServiceCityCode: "3505708", DefaultAmount: 1900000,
	}
}

// TestEnvironmentLabel confere os dois nomes de ambiente.
func TestEnvironmentLabel(t *testing.T) {
	if EnvProduction.Label() != "Produção" || EnvHomologation.Label() != "Homologação" {
		t.Fatal("rótulo de ambiente errado")
	}
}

// TestIssuerValidate aceita o emitente real e lista cada campo errado.
func TestIssuerValidate(t *testing.T) {
	if err := validIssuer().Validate(); err != nil {
		t.Fatalf("emitente real recusado: %v", err)
	}
	broken := Issuer{CNPJ: "52998224725", CityCode: "35", Series: "123456", Environment: "x", SpecialRegime: "x", SimplesTaxRate: -1}
	err := broken.Validate()
	fields, ok := err.(FieldErrors)
	if !ok {
		t.Fatalf("erro não é FieldErrors: %T", err)
	}
	for _, field := range []string{"legal_name", "cnpj", "city_code", "series", "next_dps_number", "environment", "special_regime", "simples_tax_rate"} {
		if fields[field] == "" {
			t.Errorf("faltou erro em %s", field)
		}
	}
	// Acima de 99,99% o XSD (TSDec2V2) recusa: barrar aqui, no campo.
	tooHigh := validIssuer()
	tooHigh.SimplesTaxRate = MaxPercent + 1
	if tooHigh.Validate() == nil {
		t.Error("alíquota acima de 99,99% passou")
	}
	nonDigitSeries := validIssuer()
	nonDigitSeries.Series = "A1"
	if nonDigitSeries.Validate() == nil {
		t.Error("série com letra passou")
	}
}

// TestCustomerValidate cobre tomador nacional, no exterior e e-mail inválido.
func TestCustomerValidate(t *testing.T) {
	if err := (Customer{Name: "IPÊ", Document: "56789012000100", Email: "a@b.c"}).Validate(); err != nil {
		t.Fatalf("tomador válido recusado: %v", err)
	}
	if err := (Customer{Name: "ACME Inc", Foreign: true, Country: "US"}).Validate(); err != nil {
		t.Fatalf("tomador no exterior recusado: %v", err)
	}
	fields := (Customer{Document: "123", Email: "sem-arroba"}).Validate().(FieldErrors)
	for _, field := range []string{"name", "document", "email"} {
		if fields[field] == "" {
			t.Errorf("faltou erro em %s", field)
		}
	}
	foreign := (Customer{Name: "X", Foreign: true}).Validate().(FieldErrors)
	if foreign["country"] == "" {
		t.Error("tomador no exterior sem país passou")
	}
}

// TestTemplateValidate aceita o modelo real e recusa códigos com tamanho errado.
func TestTemplateValidate(t *testing.T) {
	if err := validTemplate().Validate(); err != nil {
		t.Fatalf("modelo real recusado: %v", err)
	}
	withoutNBS := validTemplate()
	withoutNBS.NBS = ""
	if err := withoutNBS.Validate(); err != nil {
		t.Fatalf("NBS é opcional: %v", err)
	}
	fields := (Template{NationalTaxCode: "1071", NBS: "12", ServiceCityCode: "x", DefaultAmount: -1}).Validate().(FieldErrors)
	for _, field := range []string{"name", "description", "national_tax_code", "nbs", "service_city_code", "default_amount"} {
		if fields[field] == "" {
			t.Errorf("faltou erro em %s", field)
		}
	}
}

// TestFieldErrorsMessageIsStable garante mensagem em ordem fixa, apesar do mapa.
func TestFieldErrorsMessageIsStable(t *testing.T) {
	err := FieldErrors{"b": "dois", "a": "um", "c": "três"}
	if got := err.Error(); got != "a: um; b: dois; c: três" {
		t.Fatalf("got %q", got)
	}
	if (FieldErrors{}).orNil() != nil {
		t.Fatal("mapa vazio deveria virar nil")
	}
}

// TestIsDigits cobre tamanho zero, tamanho diferente e letra no meio.
func TestIsDigits(t *testing.T) {
	cases := []struct {
		text string
		size int
		want bool
	}{{"010701", 6, true}, {"", 0, false}, {"0107", 6, false}, {"01a701", 6, false}}
	for _, c := range cases {
		if got := isDigits(c.text, c.size); got != c.want {
			t.Errorf("isDigits(%q, %d) = %v", c.text, c.size, got)
		}
	}
}

// TestServiceLabelAndCode confere rótulo conhecido, desconhecido e a máscara do código.
func TestServiceLabelAndCode(t *testing.T) {
	if !strings.HasPrefix(ServiceLabel("010701"), "Suporte técnico") {
		t.Error("rótulo do 010701 errado")
	}
	if ServiceLabel("999999") != "999999" {
		t.Error("código desconhecido deveria voltar cru")
	}
	if FormatServiceCode("010701") != "01.07.01" || FormatServiceCode("17") != "17" {
		t.Error("máscara do código errada")
	}
}
