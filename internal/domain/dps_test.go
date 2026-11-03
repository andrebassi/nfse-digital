package domain

import (
	"errors"
	"testing"
	"time"
)

// dpsFixtures devolve emitente, tomador, modelo e nota fictícios e válidos.
func dpsFixtures() (Issuer, Customer, Template, Invoice) {
	issuer := Issuer{
		LegalName: "EXEMPLO TECNOLOGIA LTDA", CNPJ: "11222333000181", MunicipalRegistration: "000000",
		CityCode: "3505708", SimplesOption: "me-epp", SimplesRegime: "federal-municipal-sn", SpecialRegime: "none", SimplesTaxRate: 600,
		Environment: EnvHomologation, Series: "1", NextDPSNumber: 1002,
	}
	customer := Customer{
		Name: " IPÊ INFORMÁTICA LTDA ", Document: "56.789.012/0001-00", Email: "notas@ipe.example",
		CityCode: "3549904", ZIP: "12200000", Street: "RUA DAS FLORES", District: "CENTRO",
	}
	template := Template{NationalTaxCode: "010701", MunicipalTaxCode: "220", NBS: "115013000", ServiceCityCode: "3505708"}
	invoice := Invoice{
		Environment: EnvHomologation, DPSNumber: 1001, Amount: 1900000, Description: " Suporte ",
		IssuedAt: time.Date(2026, 10, 29, 22, 52, 0, 0, time.FixedZone("BRT", -3*3600)), Competence: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC),
	}
	return issuer, customer, template, invoice
}

// TestDPSIDMatchesReference: o Id bate com o da DPS de referência que passou
// no XSD (testdata/dps-reference.xml) e tem 45 posições; CPF usa tipo 1.
func TestDPSIDMatchesReference(t *testing.T) {
	id := DPSID("3505708", KindCNPJ, "11222333000181", "1", 1001)
	if id != "DPS350570821122233300018100001000000000001001" || len(id) != 45 {
		t.Fatalf("Id = %s (%d)", id, len(id))
	}
	if cpf := DPSID("3505708", KindCPF, "52998224725", "1", 1); cpf[10] != '1' || cpf[11:25] != "00052998224725" {
		t.Fatalf("Id de CPF = %s", cpf)
	}
}

// TestBuildDPSWithDefaults: códigos do regime, padrões de ISSQN e IBS/CBS,
// endereço do tomador e textos aparados.
func TestBuildDPSWithDefaults(t *testing.T) {
	issuer, customer, template, invoice := dpsFixtures()
	dps, err := BuildDPS(issuer, customer, template, invoice, "nfse-digital 0.1")
	if err != nil {
		t.Fatal(err)
	}
	if dps.ID != "DPS350570821122233300018100001000000000001001" || dps.Provider.SimplesOption != "3" || dps.Provider.SimplesRegime != "1" || dps.Provider.SpecialRegime != "0" {
		t.Fatalf("prestador: %+v", dps)
	}
	if dps.Taker.Document != "56789012000100" || dps.Taker.Name != "IPÊ INFORMÁTICA LTDA" || dps.Taker.Address == nil || dps.Taker.Address.Number != "S/N" {
		t.Fatalf("tomador: %+v", dps.Taker)
	}
	if dps.Values.ISSQNTaxation != "1" || dps.Values.ISSQNRetention != "1" || dps.IBSCBS != (DPSIBSCBS{"050101", "000", "000001"}) {
		t.Fatalf("padrões: %+v %+v", dps.Values, dps.IBSCBS)
	}
	if dps.Service.Description != "Suporte" || dps.Service.CityCode != "3505708" {
		t.Fatalf("serviço: %+v", dps.Service)
	}
}

// TestBuildDPSVariants: MEI sem regime de apuração, modelo com códigos
// próprios, tomador sem endereço completo e município do serviço herdado.
func TestBuildDPSVariants(t *testing.T) {
	issuer, customer, template, invoice := dpsFixtures()
	issuer.SimplesOption, issuer.SpecialRegime = "mei", "professional-society"
	customer.Street = ""
	template.ServiceCityCode, template.ISSQNRetention = "", "2"
	template.IBSCBSOperation, template.IBSCBSCST, template.IBSCBSClass = "010101", "200", "200001"
	dps, err := BuildDPS(issuer, customer, template, invoice, "x")
	if err != nil {
		t.Fatal(err)
	}
	if dps.Provider.SimplesOption != "2" || dps.Provider.SimplesRegime != "" || dps.Provider.SpecialRegime != "6" {
		t.Fatalf("regime: %+v", dps.Provider)
	}
	if dps.Taker.Address != nil || dps.Service.CityCode != "3505708" || dps.Values.ISSQNRetention != "2" || dps.IBSCBS.CST != "200" {
		t.Fatalf("variantes: %+v", dps)
	}
}

// TestBuildDPSSpecialRegimeCodes: cada regime da tela vira o código do XSD
// (TSRegEspTrib: 0 a 6 e 9). É o campo que mandou "1 — ato cooperado" para a
// SEFIN numa conta que não é cooperativa: a tabela fica travada aqui.
func TestBuildDPSSpecialRegimeCodes(t *testing.T) {
	want := map[string]string{
		"": "0", "none": "0", "cooperative-act": "1", "cooperative": "1", "estimate": "2", "municipal-microenterprise": "3",
		"notary": "4", "self-employed": "5", "professional-society": "6", "other": "9",
	}
	for regime, code := range want {
		issuer, customer, template, invoice := dpsFixtures()
		issuer.SpecialRegime = regime
		dps, err := BuildDPS(issuer, customer, template, invoice, "x")
		if err != nil || dps.Provider.SpecialRegime != code {
			t.Errorf("%q: código %q, erro %v (esperado %q)", regime, dps.Provider.SpecialRegime, err, code)
		}
	}
	if len(specialRegimeCodes) != len(want) {
		t.Errorf("a tabela tem %d regimes e o teste conhece %d: regime novo sem teste", len(specialRegimeCodes), len(want))
	}
}

// TestBuildDPSSimplesTaxRate trava a regra do E0712 (produção, 02/11/2026):
// ME/EPP leva o percentual do Simples, sem ele a DPS nem é montada; o
// "% tributos Simples" do modelo, quando preenchido, vence o do emitente;
// percentual ilegível no modelo é erro, não zero; e MEI/não optante seguem
// sem percentual (o XML manda indTotTrib 0 para eles).
func TestBuildDPSSimplesTaxRate(t *testing.T) {
	issuer, customer, template, invoice := dpsFixtures()
	dps, err := BuildDPS(issuer, customer, template, invoice, "x")
	if err != nil || dps.Values.SimplesTaxRate != "6.00" {
		t.Fatalf("emitente: %q, %v", dps.Values.SimplesTaxRate, err)
	}
	template.SimplesTaxRate = "15,5"
	if dps, err = BuildDPS(issuer, customer, template, invoice, "x"); err != nil || dps.Values.SimplesTaxRate != "15.50" {
		t.Fatalf("modelo vence o emitente: %q, %v", dps.Values.SimplesTaxRate, err)
	}
	template.SimplesTaxRate = "quinze"
	if _, err = BuildDPS(issuer, customer, template, invoice, "x"); err == nil || err.(FieldErrors)["simples_tax_rate"] == "" {
		t.Fatalf("percentual ilegível no modelo passou: %v", err)
	}
	template.SimplesTaxRate = ""
	issuer.SimplesTaxRate = 0
	if _, err = BuildDPS(issuer, customer, template, invoice, "x"); err == nil || err.(FieldErrors)["simples_tax_rate"] == "" {
		t.Fatalf("ME/EPP sem alíquota passou: %v", err)
	}
	for _, option := range []string{"mei", "none"} {
		issuer.SimplesOption = option
		if dps, err = BuildDPS(issuer, customer, template, invoice, "x"); err != nil || dps.Values.SimplesTaxRate != "" {
			t.Errorf("%s: %q, %v (sem percentual)", option, dps.Values.SimplesTaxRate, err)
		}
	}
}

// TestBuildDPSRejectsBadInput lista todos os erros de cadastro de uma vez e
// recusa tomador no exterior.
func TestBuildDPSRejectsBadInput(t *testing.T) {
	issuer, customer, template, invoice := dpsFixtures()
	if _, err := BuildDPS(issuer, Customer{Foreign: true}, template, invoice, "x"); !errors.Is(err, ErrForeignNotSupported) {
		t.Fatalf("exterior: %v", err)
	}
	issuer.CNPJ, issuer.SimplesOption, issuer.SpecialRegime = "", "x", "x"
	customer.Document = "123"
	template.NationalTaxCode = "17"
	invoice.Amount, invoice.DPSNumber, invoice.Description = 0, 0, " "
	fields := func() FieldErrors {
		_, err := BuildDPS(issuer, customer, template, invoice, "x")
		return err.(FieldErrors)
	}()
	for _, field := range []string{"issuer", "customer", "template", "amount", "dps_number", "description", "simples_option", "special_regime"} {
		if fields[field] == "" {
			t.Errorf("faltou erro em %s", field)
		}
	}
	issuer2, customer2, template2, invoice2 := dpsFixtures()
	issuer2.SimplesRegime = "x"
	if _, err := BuildDPS(issuer2, customer2, template2, invoice2, "x"); err == nil {
		t.Error("regime de apuração inválido passou")
	}
}

// TestFormatDecimalAndAmbient confere o formato do valor e o tpAmb.
func TestFormatDecimalAndAmbient(t *testing.T) {
	if Money(1900000).FormatDecimal() != "19000.00" || Money(5).FormatDecimal() != "0.05" {
		t.Fatal("FormatDecimal")
	}
	if EnvProduction.AmbientCode() != "1" || EnvHomologation.AmbientCode() != "2" {
		t.Fatal("AmbientCode")
	}
	if firstNonEmpty("", "  ") != "" {
		t.Fatal("firstNonEmpty sem valor deveria devolver vazio")
	}
	// Trava o E1235 de 29/10/2026: verAplic acima de 20 caracteres reprova na SEFIN.
	issuer, customer, template, invoice := dpsFixtures()
	dps, _ := BuildDPS(issuer, customer, template, invoice, "nfse-digital 0.1.0-prototype-ção")
	if len([]rune(dps.AppVersion)) != 20 {
		t.Fatalf("verAplic com %d caracteres", len([]rune(dps.AppVersion)))
	}
}
