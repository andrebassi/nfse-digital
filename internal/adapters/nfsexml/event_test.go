package nfsexml

import (
	"errors"
	"testing"
)

// eventXML é o formato real do evento da distribuição (30/10/2026), com
// números fictícios.
const eventXML = `<?xml version="1.0" encoding="utf-8"?><evento versao="1.01" xmlns="http://www.sped.fazenda.gov.br/nfse"><infEvento Id="EVT1"><nSeqEvento>001</nSeqEvento><pedRegEvento versao="1.01"><infPedReg Id="PRE1"><tpAmb>1</tpAmb><CNPJAutor>11222333000181</CNPJAutor><chNFSe>35057081211222333000181000000000003826101922066208</chNFSe><e101101><xDesc>Cancelamento de NFS-e</xDesc><cMotivo>1</cMotivo><xMotivo>CANCELAMENTO DO SERVIÇO</xMotivo></e101101></infPedReg></pedRegEvento></infEvento></evento>`

// TestParseEvent: cancelamento real, evento que não cancela e evento sem chave.
func TestParseEvent(t *testing.T) {
	key, cancels, err := ParseEvent([]byte(eventXML))
	if err != nil || !cancels || key != "35057081211222333000181000000000003826101922066208" {
		t.Fatalf("cancelamento: %q %v %v", key, cancels, err)
	}
	other := []byte(`<evento><infEvento><pedRegEvento><infPedReg><chNFSe>K</chNFSe><e105104/></infPedReg></pedRegEvento></infEvento></evento>`)
	if key, cancels, err := ParseEvent(other); err != nil || cancels || key != "K" {
		t.Fatalf("outro evento: %q %v %v", key, cancels, err)
	}
	for _, bad := range []string{`<evento>`, `<evento><infEvento/></evento>`} {
		if _, _, err := ParseEvent([]byte(bad)); !errors.Is(err, ErrNotNFSe) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
