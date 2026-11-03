package nfsexml

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"nfse/internal/domain"
)

// testdata lê um arquivo da pasta testdata/ na raiz do projeto.
func testdata(t *testing.T, name string) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestParseAuthorizedNFSe lê a NFS-e de referência (estrutura real, dados
// fictícios) e confere cada parte importada.
func TestParseAuthorizedNFSe(t *testing.T) {
	imported, err := Parse(testdata(t, "nfse-reference.xml"))
	if err != nil {
		t.Fatal(err)
	}
	issuer, customer, template, invoice := imported.Issuer, imported.Customer, imported.Template, imported.Invoice
	if issuer.CNPJ != "11222333000181" || issuer.LegalName != "EXEMPLO TECNOLOGIA LTDA" || issuer.MunicipalRegistration != "000000" ||
		issuer.CityName != "Barueri" || issuer.SimplesOption != "me-epp" || issuer.SimplesRegime != "federal-municipal-sn" ||
		issuer.SpecialRegime != "none" || issuer.Series != "1" || issuer.NextDPSNumber != 202627 {
		t.Fatalf("emitente: %+v", issuer)
	}
	if customer.Document != "56789012000100" || customer.Name != "IPÊ INFORMÁTICA LTDA" || customer.CityCode != "3549904" || customer.Street != "RUA DAS FLORES" {
		t.Fatalf("tomador: %+v", customer)
	}
	if template.NationalTaxCode != "010701" || template.MunicipalTaxCode != "220" || template.NBS != "115013000" || template.DefaultAmount != 1900000 ||
		template.IBSCBSOperation != "050101" || template.IBSCBSCST != "000" || template.IBSCBSClass != "000001" || template.ISSQNTaxation != "1" {
		t.Fatalf("modelo: %+v", template)
	}
	if invoice.Status != domain.StatusAuthorized || invoice.NFSeNumber != "38" || len(invoice.AccessKey) != 50 || invoice.Environment != domain.EnvProduction ||
		invoice.Competence.Format("2006-01-02") != "2026-10-05" || invoice.IssuedAt.IsZero() {
		t.Fatalf("nota: %+v", invoice)
	}
	if imported.HasSignature {
		t.Fatal("a referência fictícia não tem assinatura")
	}
}

// TestParseDPS: DPS solta vira só cadastro, sem nota; com o leiaute da
// homologação (trib sem gIBSCBS) os códigos IBS/CBS também são lidos.
func TestParseDPS(t *testing.T) {
	imported, err := Parse(testdata(t, "dps-reference.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if imported.Invoice.AccessKey != "" || imported.Customer.Document != "56789012000100" || imported.Template.IBSCBSCST != "000" {
		t.Fatalf("DPS: %+v", imported)
	}
	restricted := strings.Replace(string(testdata(t, "dps-reference.xml")), "<gIBSCBS><CST>000</CST><cClassTrib>000001</cClassTrib></gIBSCBS>", "<CST>200</CST><cClassTrib>200001</cClassTrib>", 1)
	imported, err = Parse([]byte(restricted))
	if err != nil || imported.Template.IBSCBSCST != "200" || imported.Template.IBSCBSClass != "200001" {
		t.Fatalf("leiaute restrito: %+v, %v", imported.Template, err)
	}
}

// TestParseSpecialRegimes: cada código de regEspTrib de uma nota importada vira
// o valor da tela. Antes, 3, 4, 5 e 9 viravam texto vazio, e a conta criada
// pela importação gravava "Nenhum" no lugar do regime real.
func TestParseSpecialRegimes(t *testing.T) {
	want := map[string]string{"0": "none", "1": "cooperative-act", "2": "estimate", "3": "municipal-microenterprise",
		"4": "notary", "5": "self-employed", "6": "professional-society", "9": "other"}
	for code, regime := range want {
		xml := strings.Replace(string(testdata(t, "dps-reference.xml")), "<regEspTrib>0</regEspTrib>", "<regEspTrib>"+code+"</regEspTrib>", 1)
		imported, err := Parse([]byte(xml))
		if err != nil || imported.Issuer.SpecialRegime != regime {
			t.Errorf("código %s: %q, erro %v (esperado %q)", code, imported.Issuer.SpecialRegime, err, regime)
		}
	}
}

// TestParseRejects: XML quebrado, outro documento, DPS sem Id e valor ilegível.
func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"truncado":       `<NFSe><infNFSe>`,
		"outro leiaute":  `<nfeProc><NFe/></nfeProc>`,
		"DPS sem Id":     `<DPS><infDPS><prest><CNPJ>1</CNPJ></prest></infDPS></DPS>`,
		"valor ilegível": `<DPS><infDPS Id="DPS1"><prest><CNPJ>1</CNPJ></prest><valores><vServPrest><vServ>abc</vServ></vServPrest></valores></infDPS></DPS>`,
		"NFSe quebrada":  `<NFSe><infNFSe><DPS><infDPS Id="x"></DPS></infNFSe></NFSe>`,
		"DPS quebrada":   `<DPS><infDPS></DPS>`,
		"NFSe sem DPS":   `<NFSe><infNFSe Id="NFS1"></infNFSe></NFSe>`,
	}
	for name, body := range cases {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: deveria falhar", name)
		} else if name != "valor ilegível" && !errors.Is(err, ErrNotNFSe) {
			t.Errorf("%s: erro sem ErrNotNFSe: %v", name, err)
		}
	}
}

// TestTitleCity padroniza o nome do município.
func TestTitleCity(t *testing.T) {
	if got := titleCity("SAO JOSE DOS CAMPOS"); got != "Sao Jose dos Campos" {
		t.Fatalf("got %q", got)
	}
}
