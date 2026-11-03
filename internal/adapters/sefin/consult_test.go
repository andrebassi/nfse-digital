package sefin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nfse/internal/ports"
)

// TestConsulter: chave pela DPS (achada, não registrada, resposta estranha e
// rede fora) e documentos da nota pelo ADN.
func TestConsulter(t *testing.T) {
	certificate := testCertificate(t)
	server := newFakeADN(t, map[string]struct {
		status int
		body   string
	}{
		"/dps/D1":                        {200, `{"chaveAcesso":"K1"}`},
		"/dps/D2":                        {404, `{"erro":{"codigo":"E2404","descricao":"Não foi gerada uma NFS-e"}}`},
		"/dps/D3":                        {200, `{}`},
		"/nfse/K1":                       {200, `{"nfseXmlGZipB64":"` + packedNFSe() + `"}`},
		"/nfse/K2":                       {404, `{}`},
		"/nfse/K3":                       {200, `{"outro":1}`},
		"/contribuintes/NFSe/K1/Eventos": {200, `{"StatusProcessamento":"DOCUMENTOS_LOCALIZADOS","LoteDFe":[{"NSU":26,"ChaveAcesso":"K1","TipoDocumento":"NFSE","ArquivoXml":"` + packedNFSe() + `"}]}`},
	})
	consulter := Consulter{Reader: NewReader(certificate.TLS, rootsOf(server)), SefinURL: server.URL, ADNURL: server.URL}
	ctx := context.Background()

	if key, err := consulter.AccessKeyByDPS(ctx, "D1"); err != nil || key != "K1" {
		t.Fatalf("achada: %q, %v", key, err)
	}
	if _, err := consulter.AccessKeyByDPS(ctx, "D2"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("não registrada: %v", err)
	}
	if _, err := consulter.AccessKeyByDPS(ctx, "D3"); err == nil {
		t.Fatal("resposta sem chave deveria falhar")
	}
	batch, err := consulter.Documents(ctx, "K1")
	if err != nil || len(batch.Documents) != 1 || batch.Documents[0].Type != "NFSE" {
		t.Fatalf("documentos: %+v, %v", batch, err)
	}

	// XML da nota pela chave: achada, inexistente e resposta sem o campo.
	if xml, err := consulter.NFSeXML(ctx, "K1"); err != nil || !strings.Contains(string(xml), "<nNFSe>42</nNFSe>") {
		t.Fatalf("XML: %s, %v", xml, err)
	}
	if _, err := consulter.NFSeXML(ctx, "K2"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("nota inexistente: %v", err)
	}
	if _, err := consulter.NFSeXML(ctx, "K3"); err == nil {
		t.Fatal("resposta sem XML deveria falhar")
	}

	offline := Consulter{Reader: NewReader(certificate.TLS, nil), SefinURL: "https://127.0.0.1:1", ADNURL: "https://127.0.0.1:1"}
	if _, err := offline.AccessKeyByDPS(ctx, "D1"); !errors.Is(err, ErrTransient) {
		t.Fatalf("rede fora na DPS: %v", err)
	}
	if _, err := offline.Documents(ctx, "K1"); !errors.Is(err, ErrTransient) {
		t.Fatalf("rede fora nos documentos: %v", err)
	}
	if _, err := offline.NFSeXML(ctx, "K1"); !errors.Is(err, ErrTransient) {
		t.Fatalf("rede fora no XML: %v", err)
	}
}
