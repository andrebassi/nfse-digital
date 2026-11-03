package sefin

import (
	"context"
	"encoding/json"
	"fmt"

	"nfse/internal/ports"
)

// Consulter lê o estado de uma nota (só leitura). Rotas medidas na produção
// em 30/10/2026 (docs/sefin-notes.md, seção 12):
//   - SEFIN GET /dps/{Id da DPS} → {"chaveAcesso": …}; 404 E2404 = não registrada;
//   - ADN GET /contribuintes/NFSe/{chave}/Eventos → lote com a NFS-e e os eventos.
type Consulter struct {
	Reader   *Reader
	SefinURL string
	ADNURL   string
}

// compile-time: garante que o adapter cumpre o port.
var _ ports.Consulter = Consulter{}

// AccessKeyByDPS devolve a chave da NFS-e gerada pela DPS, ou ErrNotFound.
func (c Consulter) AccessKeyByDPS(ctx context.Context, dpsID string) (string, error) {
	result, err := c.Reader.Get(ctx, c.SefinURL+"/dps/"+dpsID)
	if err != nil {
		return "", err
	}
	if result.StatusCode == 404 {
		return "", ports.ErrNotFound
	}
	var payload struct {
		ChaveAcesso string `json:"chaveAcesso"`
	}
	if result.StatusCode != 200 || json.Unmarshal(result.Body, &payload) != nil || payload.ChaveAcesso == "" {
		return "", fmt.Errorf("consulta da DPS respondeu HTTP %d: %.200s", result.StatusCode, result.Body)
	}
	return payload.ChaveAcesso, nil
}

// NFSeXML devolve o XML da NFS-e pela chave (GET /nfse/{chave} na SEFIN,
// resposta {"nfseXmlGZipB64": …}, medida em 30/10/2026).
func (c Consulter) NFSeXML(ctx context.Context, accessKey string) ([]byte, error) {
	result, err := c.Reader.Get(ctx, c.SefinURL+"/nfse/"+accessKey)
	if err != nil {
		return nil, err
	}
	if result.StatusCode == 404 {
		return nil, ports.ErrNotFound
	}
	var payload struct {
		NFSeXMLGZipB64 string `json:"nfseXmlGZipB64"`
	}
	if result.StatusCode != 200 || json.Unmarshal(result.Body, &payload) != nil || payload.NFSeXMLGZipB64 == "" {
		return nil, fmt.Errorf("consulta da NFS-e respondeu HTTP %d: %.200s", result.StatusCode, result.Body)
	}
	return unpack(payload.NFSeXMLGZipB64)
}

// Documents devolve a NFS-e e os eventos dela (cancelamento, por exemplo).
func (c Consulter) Documents(ctx context.Context, accessKey string) (ports.DFeBatch, error) {
	distributor := Distributor{Reader: c.Reader}
	// Mesmo formato de lote da distribuição; reaproveita o leitor dela.
	result, err := c.Reader.Get(ctx, c.ADNURL+"/contribuintes/NFSe/"+accessKey+"/Eventos")
	if err != nil {
		return ports.DFeBatch{}, err
	}
	return distributor.parse(result)
}
