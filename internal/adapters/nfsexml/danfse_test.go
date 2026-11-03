package nfsexml

import (
	"errors"
	"testing"
)

// TestParseDANFSe confere os campos impressos lidos da NFS-e de referência.
func TestParseDANFSe(t *testing.T) {
	data, err := ParseDANFSe(testdata(t, "nfse-reference.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if data.Number != "38" || len(data.AccessKey) != 50 || data.Provider.Document != "11222333000181" || data.Provider.Name != "EXEMPLO TECNOLOGIA LTDA" {
		t.Fatalf("cabeçalho/prestador: %+v", data)
	}
	if data.Taker.Document != "56789012000100" || data.Taker.Address != "RUA DAS FLORES, 100, SALA 1, CENTRO" || data.Taker.City != "3549904" {
		t.Fatalf("tomador: %+v", data.Taker)
	}
	if data.Amount != "19000.00" || data.Rate == "" || data.ISSQN == "" || data.Retention != "Não retido" || data.Environment != "Produção" {
		t.Fatalf("valores: %+v", data)
	}
	if data.ServiceCode != "010701" || data.IncidenceCity != "Barueri" || data.IBSCBSTotal == "" || data.Competence.IsZero() || data.ProcessedAt.IsZero() {
		t.Fatalf("serviço/IBS: %+v", data)
	}
	if _, err := ParseDANFSe([]byte("<DPS/>")); !errors.Is(err, ErrNotNFSe) {
		t.Fatalf("DPS solta deveria falhar: %v", err)
	}
	homologation, _ := ParseDANFSe([]byte(`<NFSe><infNFSe Id="NFS1"><DPS><infDPS><tpAmb>2</tpAmb></infDPS></DPS></infNFSe></NFSe>`))
	if homologation.Environment != "Homologação — sem valor fiscal" {
		t.Fatalf("ambiente: %q", homologation.Environment)
	}
}
