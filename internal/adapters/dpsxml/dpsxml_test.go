package dpsxml

import (
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"

	"nfse/internal/domain"
)

// brt é UTC-3 fixo, para o teste não depender do tzdata da máquina.
var brt = time.FixedZone("BRT", -3*60*60)

// fictitiousDPS monta a DPS fictícia (mesmos dados da testdata/dps-reference.xml).
func fictitiousDPS(t *testing.T) domain.DPS {
	t.Helper()
	issuer := domain.Issuer{
		LegalName: "EXEMPLO TECNOLOGIA LTDA", CNPJ: "11222333000181", MunicipalRegistration: "000000", CityCode: "3505708",
		SimplesOption: "me-epp", SimplesRegime: "federal-municipal-sn", SpecialRegime: "none", SimplesTaxRate: 600, Environment: domain.EnvHomologation,
		Series: "1", NextDPSNumber: 1002,
	}
	customer := domain.Customer{
		Name: "IPÊ INFORMÁTICA LTDA", Document: "56789012000100", Email: "notas@ipe.example", CityCode: "3549904",
		ZIP: "12200000", Street: "RUA DAS FLORES", Number: "100", Complement: "SALA 1", District: "CENTRO",
	}
	template := domain.Template{NationalTaxCode: "010701", MunicipalTaxCode: "220", NBS: "115013000", ServiceCityCode: "3505708"}
	invoice := domain.Invoice{
		Environment: domain.EnvHomologation, DPSNumber: 1001, Amount: 1900000, Description: "Suporte técnico em informática & bancos de dados <teste>",
		IssuedAt: time.Date(2026, 10, 29, 22, 52, 0, 0, brt), Competence: time.Date(2026, 10, 29, 0, 0, 0, 0, brt),
	}
	dps, err := domain.BuildDPS(issuer, customer, template, invoice, "nfse-digital 0.1.0-prototype-versao-longa")
	if err != nil {
		t.Fatal(err)
	}
	return dps
}

// testKey gera um par de chave e certificado autoassinado para assinar.
func testKey(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "EXEMPLO TECNOLOGIA LTDA:11222333000181"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	certificate, _ := x509.ParseCertificate(der)
	return key, certificate
}

// projectRoot acha a raiz do módulo a partir deste arquivo.
func projectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// tool roda uma ferramenta de linha de comando; sem ela no PATH, usa o nix
// shell, que é como o projeto declara as dependências (flake.nix).
func tool(t *testing.T, pkg, name string, args ...string) (string, error) {
	t.Helper()
	var command *exec.Cmd
	if _, err := exec.LookPath(name); err == nil {
		command = exec.Command(name, args...)
	} else {
		command = exec.Command("nix", append([]string{"shell", "nixpkgs#" + pkg, "--command", name}, args...)...)
	}
	output, err := command.CombinedOutput()
	return string(output), err
}

// TestBuildValidatesAgainstXSD: em cada ambiente, a DPS sem assinatura e a
// assinada passam no XSD daquele ambiente, e reprovam no do outro (prova de
// que os dois leiautes são mesmo diferentes e de que o teste enxerga isso).
func TestBuildValidatesAgainstXSD(t *testing.T) {
	key, certificate := testKey(t)
	schemas := map[domain.Environment]string{
		domain.EnvProduction:   filepath.Join(projectRoot(), "schemas", "v1.01-xmllint", "DPS_v1.01.xsd"),
		domain.EnvHomologation: filepath.Join(projectRoot(), "schemas", "prod-restrita-20261020-xmllint", "DPS_v1.01.xsd"),
	}
	for environment, schema := range schemas {
		dps := fictitiousDPS(t)
		dps.Environment = environment
		unsigned, err := Marshal(Build(dps))
		if err != nil {
			t.Fatal(err)
		}
		signed, _, err := Encode(dps, key, [][]byte{certificate.Raw})
		if err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string][]byte{"sem-assinatura.xml": unsigned, "assinada.xml": signed} {
			path := filepath.Join(t.TempDir(), name)
			_ = os.WriteFile(path, content, 0o600)
			if output, err := tool(t, "libxml2", "xmllint", "--noout", "--schema", schema, path); err != nil {
				t.Errorf("%s %s reprovou no XSD do ambiente: %s", environment, name, output)
			}
			for other, otherSchema := range schemas {
				if other != environment {
					if _, err := tool(t, "libxml2", "xmllint", "--noout", "--schema", otherSchema, path); err == nil {
						t.Errorf("%s %s passou no XSD de %s: os leiautes deveriam diferir", environment, name, other)
					}
				}
			}
		}
	}
}

// TestSignatureLayout: assinatura irmã do infDPS, sem prefixo, com os
// algoritmos da SEFIN e referência ao Id.
func TestSignatureLayout(t *testing.T) {
	key, certificate := testKey(t)
	xml, _, err := Encode(fictitiousDPS(t), key, [][]byte{certificate.Raw})
	if err != nil {
		t.Fatal(err)
	}
	body := string(xml)
	for _, want := range []string{
		`</infDPS><Signature xmlns="http://www.w3.org/2000/09/xmldsig#">`,
		`Algorithm="http://www.w3.org/2000/09/xmldsig#rsa-sha1"`,
		`Algorithm="http://www.w3.org/2000/09/xmldsig#sha1"`,
		`Algorithm="http://www.w3.org/2000/09/xmldsig#enveloped-signature"`,
		`Algorithm="http://www.w3.org/TR/2001/REC-xml-c14n-20010315"`,
		`URI="#DPS350570821122233300018100001000000000001001"`,
		`<vServ>19000.00</vServ>`,
		`&amp; bancos de dados &lt;teste&gt;`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("faltou %s", want)
		}
	}
	if strings.Contains(body, "ds:") || strings.Contains(body, "\n<") {
		t.Error("prefixo ds: ou quebra de linha entre elementos")
	}
}

// verifyWithLibrary confere a assinatura pelo goxmldsig. A biblioteca só valida
// assinatura DENTRO do elemento; como a transformação enveloped descarta a
// assinatura do cálculo, mover uma cópia para dentro do infDPS não muda o digest.
func verifyWithLibrary(xml []byte, certificate *x509.Certificate) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(xml); err != nil {
		return err
	}
	inf := doc.Root().SelectElement("infDPS").Copy()
	// Fora do <DPS> a cópia perde o xmlns herdado, que o C14N inclusivo conta.
	inf.CreateAttr("xmlns", Namespace)
	inf.AddChild(doc.Root().SelectElement("Signature").Copy())
	ctx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{certificate}})
	ctx.IdAttribute = "Id"
	ctx.Clock = dsig.NewFakeClockAt(time.Now())
	_, err := ctx.Validate(inf)
	return err
}

// TestSignatureVerifiesAndDetectsTampering: a assinatura confere pela
// biblioteca e pelo xmlsec1 (ferramenta independente); trocar o valor da nota
// depois de assinar faz as duas reprovarem.
func TestSignatureVerifiesAndDetectsTampering(t *testing.T) {
	key, certificate := testKey(t)
	xml, _, err := Encode(fictitiousDPS(t), key, [][]byte{certificate.Raw})
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(xml, []byte("<vServ>19000.00</vServ>"), []byte("<vServ>19000.01</vServ>"), 1)

	if err := verifyWithLibrary(xml, certificate); err != nil {
		t.Fatalf("goxmldsig recusou a assinatura: %v", err)
	}
	if err := verifyWithLibrary(tampered, certificate); err == nil {
		t.Fatal("goxmldsig aceitou o XML adulterado")
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	_ = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), 0o600)
	for name, content := range map[string][]byte{"ok.xml": xml, "adulterado.xml": tampered} {
		path := filepath.Join(dir, name)
		_ = os.WriteFile(path, content, 0o600)
		output, err := tool(t, "xmlsec", "xmlsec1", "--verify", "--id-attr:Id", Namespace+":infDPS", "--trusted-pem", certPath, path)
		if name == "ok.xml" && err != nil {
			t.Errorf("xmlsec1 recusou a assinatura: %s", output)
		}
		if name == "adulterado.xml" && err == nil {
			t.Errorf("xmlsec1 aceitou o XML adulterado: %s", output)
		}
	}
}

// TestPackRoundTrip: gzip+base64 volta ao XML original.
func TestPackRoundTrip(t *testing.T) {
	packed, err := Pack([]byte("<DPS/>"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(packed)
	reader, _ := gzip.NewReader(bytes.NewReader(raw))
	back, _ := io.ReadAll(reader)
	if string(back) != "<DPS/>" {
		t.Fatalf("voltou %q", back)
	}
}

// TestOptionalFieldsAndCPF: campo opcional vazio não aparece; tomador CPF usa <CPF>.
func TestOptionalFieldsAndCPF(t *testing.T) {
	dps := fictitiousDPS(t)
	dps.Taker.DocumentKind, dps.Taker.Document, dps.Taker.Address, dps.Taker.Email = domain.KindCPF, "52998224725", nil, ""
	dps.Provider.MunicipalRegistration, dps.Provider.SimplesRegime = "", ""
	xml, _ := Marshal(Build(dps))
	body := string(xml)
	if !strings.Contains(body, "<CPF>52998224725</CPF>") || strings.Contains(body, "<IM>") || strings.Contains(body, "<end>") || strings.Contains(body, "<email>") || strings.Contains(body, "<regApTribSN>") {
		t.Fatalf("opcionais: %s", body)
	}
}

// TestTotalTaxChoice: o totTrib é uma escolha do XSD e só um ramo sai. ME/EPP
// manda pTotTribSN — indTotTrib ali voltou E0712 em produção (02/11/2026) —
// e o resto manda indTotTrib 0. Os dois ramos passam no XSD dos dois
// ambientes em TestBuildValidatesAgainstXSD (fixture ME/EPP) e aqui.
func TestTotalTaxChoice(t *testing.T) {
	dps := fictitiousDPS(t)
	xml, _ := Marshal(Build(dps))
	if body := string(xml); !strings.Contains(body, "<totTrib><pTotTribSN>6.00</pTotTribSN></totTrib>") || strings.Contains(body, "indTotTrib") {
		t.Fatalf("ME/EPP: %s", body)
	}
	dps.Values.SimplesTaxRate = ""
	xml, _ = Marshal(Build(dps))
	if body := string(xml); !strings.Contains(body, "<totTrib><indTotTrib>0</indTotTrib></totTrib>") || strings.Contains(body, "pTotTribSN") {
		t.Fatalf("sem percentual: %s", body)
	}
	path := filepath.Join(t.TempDir(), "indTotTrib.xml")
	if err := os.WriteFile(path, xml, 0o600); err != nil {
		t.Fatal(err)
	}
	schema := filepath.Join(projectRoot(), "schemas", "prod-restrita-20261020-xmllint", "DPS_v1.01.xsd")
	if output, err := tool(t, "libxml2", "xmllint", "--noout", "--schema", schema, path); err != nil {
		t.Fatalf("indTotTrib fora do XSD: %v\n%s", err, output)
	}
}

// failingSigner devolve erro ao assinar, como uma aprovação negada no celular.
type failingSigner struct{ public crypto.PublicKey }

// Public devolve a chave pública verdadeira, para a preparação passar.
func (f failingSigner) Public() crypto.PublicKey { return f.public }

// Sign sempre falha.
func (failingSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("assinatura negada")
}

// TestSignErrors: falha do assinador vira erro, sem XML pela metade.
func TestSignErrors(t *testing.T) {
	key, certificate := testKey(t)
	xml, packed, err := Encode(fictitiousDPS(t), failingSigner{&key.PublicKey}, [][]byte{certificate.Raw})
	if err == nil || xml != nil || packed != "" {
		t.Fatalf("deveria falhar sem devolver XML: %v", err)
	}
}
