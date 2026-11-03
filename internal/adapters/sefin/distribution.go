package sefin

import (
	"context"
	"encoding/json"
	"fmt"

	"nfse/internal/ports"
)

// Distributor lê a distribuição de documentos do ADN (GET
// /contribuintes/DFe/{NSU}), que devolve até 50 documentos depois do NSU
// informado. Formato medido em 30/10/2026 (docs/sefin-notes.md, seção 12).
type Distributor struct {
	Reader  *Reader
	BaseURL string
}

// compile-time: garante que o adapter cumpre o port.
var _ ports.PortalReader = Distributor{}

// distributionResponse é o corpo da resposta, nos nomes que o ADN usa.
type distributionResponse struct {
	StatusProcessamento string
	LoteDFe             []struct {
		NSU           int64
		ChaveAcesso   string
		TipoDocumento string
		ArquivoXml    string
	}
	Erros []struct {
		Codigo    string
		Descricao string
	}
}

// Distribution lê o lote depois de afterNSU. "NENHUM_DOCUMENTO_LOCALIZADO"
// (ou lote vazio) encerra a leitura; lote cheio indica que há mais.
func (d Distributor) Distribution(ctx context.Context, afterNSU int64) (ports.DFeBatch, error) {
	result, err := d.Reader.Get(ctx, fmt.Sprintf("%s/contribuintes/DFe/%d", d.BaseURL, afterNSU))
	if err != nil {
		return ports.DFeBatch{}, err
	}
	return d.parse(result)
}

// parse interpreta um lote do ADN (distribuição ou eventos de uma nota).
func (d Distributor) parse(result ReadResult) (ports.DFeBatch, error) {
	var response distributionResponse
	if err := json.Unmarshal(result.Body, &response); err != nil {
		return ports.DFeBatch{}, fmt.Errorf("resposta do ADN ilegível (HTTP %d): %w", result.StatusCode, err)
	}
	if result.StatusCode >= 400 && len(response.LoteDFe) == 0 {
		// E2220 "nenhum documento a partir do NSU" é o fim da distribuição, não
		// falha (medido em 30/10/2026, segunda página depois do NSU 49).
		if len(response.Erros) > 0 && response.Erros[0].Codigo == "E2220" {
			return ports.DFeBatch{}, nil
		}
		if len(response.Erros) > 0 {
			return ports.DFeBatch{}, fmt.Errorf("ADN recusou: [%s] %s", response.Erros[0].Codigo, response.Erros[0].Descricao)
		}
		// 404 com corpo JSON sem erro é o "nenhum documento" de algumas versões.
		return ports.DFeBatch{}, nil
	}
	batch := ports.DFeBatch{}
	for _, item := range response.LoteDFe {
		xml, err := unpack(item.ArquivoXml)
		if err != nil {
			return ports.DFeBatch{}, fmt.Errorf("documento NSU %d ilegível: %w", item.NSU, err)
		}
		batch.Documents = append(batch.Documents, ports.DFeDocument{NSU: item.NSU, AccessKey: item.ChaveAcesso, Type: item.TipoDocumento, XML: xml})
	}
	batch.More = response.StatusProcessamento == "DOCUMENTOS_LOCALIZADOS" && len(batch.Documents) > 0
	return batch, nil
}
