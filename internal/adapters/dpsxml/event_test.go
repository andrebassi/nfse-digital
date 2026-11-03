package dpsxml

import (
	"bytes"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nfse/internal/domain"
)

// fictitiousCancel é um pedido de cancelamento de nota fictícia.
func fictitiousCancel(environment domain.Environment) domain.CancelEvent {
	return domain.CancelEvent{
		ID:          "PRE" + "35057082112223330001810000000000003826100000000001" + domain.CancelEventType,
		Environment: environment,
		AppVersion:  "nfse-digital 0.1",
		At:          time.Date(2026, 10, 30, 9, 0, 0, 0, time.FixedZone("BRT", -3*3600)),
		AuthorCNPJ:  "11222333000181",
		AccessKey:   "35057082112223330001810000000000003826100000000001",
		ReasonCode:  "1",
		Description: "valor digitado errado na emissão",
	}
}

// TestCancelValidatesAgainstXSD: o pedido assinado passa no XSD
// pedRegEvento dos dois pacotes, e um motivo fora da lista reprova (prova de
// que o teste enxerga o esquema).
func TestCancelValidatesAgainstXSD(t *testing.T) {
	key, certificate := testKey(t)
	for environment, dir := range map[domain.Environment]string{domain.EnvProduction: "v1.01-xmllint", domain.EnvHomologation: "prod-restrita-20261020-xmllint"} {
		schema := filepath.Join(projectRoot(), "schemas", dir, "pedRegEvento_v1.01.xsd")
		signed, packed, err := EncodeCancel(fictitiousCancel(environment), key, [][]byte{certificate.Raw})
		if err != nil || packed == "" {
			t.Fatal(err)
		}
		bad := bytes.Replace(signed, []byte("<cMotivo>1</cMotivo>"), []byte("<cMotivo>5</cMotivo>"), 1)
		for name, content := range map[string][]byte{"ok.xml": signed, "motivo-invalido.xml": bad} {
			path := filepath.Join(t.TempDir(), name)
			_ = os.WriteFile(path, content, 0o600)
			output, err := tool(t, "libxml2", "xmllint", "--noout", "--schema", schema, path)
			if name == "ok.xml" && err != nil {
				t.Errorf("%s: pedido reprovou no XSD: %s", environment, output)
			}
			if name == "motivo-invalido.xml" && err == nil {
				t.Errorf("%s: motivo 5 passou no XSD", environment)
			}
		}
	}
}

// TestCancelSignatureVerifies: a assinatura referencia o infPedReg e o xmlsec1
// a aceita; adulterar a justificativa invalida.
func TestCancelSignatureVerifies(t *testing.T) {
	key, certificate := testKey(t)
	signed, _, err := EncodeCancel(fictitiousCancel(domain.EnvHomologation), key, [][]byte{certificate.Raw})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(signed, []byte(`<Reference URI="#PRE`)) || !bytes.Contains(signed, []byte("<xDesc>Cancelamento de NFS-e</xDesc>")) {
		t.Fatalf("pedido sem referência ao Id ou sem xDesc: %s", signed)
	}
	tampered := bytes.Replace(signed, []byte("valor digitado"), []byte("valor alterado"), 1)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	_ = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), 0o600)
	for name, content := range map[string][]byte{"ok.xml": signed, "adulterado.xml": tampered} {
		path := filepath.Join(dir, name)
		_ = os.WriteFile(path, content, 0o600)
		output, err := tool(t, "xmlsec", "xmlsec1", "--verify", "--id-attr:Id", Namespace+":infPedReg", "--trusted-pem", certPath, path)
		if name == "ok.xml" && err != nil {
			t.Errorf("xmlsec1 recusou: %s", output)
		}
		if name == "adulterado.xml" && err == nil {
			t.Error("xmlsec1 aceitou o pedido adulterado")
		}
	}
	if _, _, err := EncodeCancel(fictitiousCancel(domain.EnvHomologation), nil, nil); err == nil {
		t.Fatal("sem chave deveria falhar")
	}
}
