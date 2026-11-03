// Package simulated responde como a SEFIN responderia, sem rede. É o dublê de
// emissor dos testes automáticos: nenhuma nota sai daqui para o governo, e o
// painel nunca o usa.
package simulated

import (
	"context"
	"fmt"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// barueriCityCode é o município que a SEFIN real recusa com E0039, porque
// Barueri manteve emissor próprio (medido em 28/10/2026).
const barueriCityCode = "3505708"

// e0039Message é o texto literal que a SEFIN devolveu na nota de 28/10/2026.
const e0039Message = "O município emissor informado na DPS deve estar parametrizado para utilizar os emissores públicos nacionais, conforme parametrização do município no Sistema Nacional NFS-e."

// Emitter devolve a resposta que a SEFIN daria: E0039 para Barueri e
// autorização fictícia para os demais municípios.
type Emitter struct{}

// compile-time: garante que o adapter cumpre o port.
var _ ports.Emitter = Emitter{}

// Emit simula o envio. A chave fictícia começa com SIM para nunca ser
// confundida com uma chave real, que tem 50 dígitos.
func (Emitter) Emit(_ context.Context, request ports.EmitRequest) (ports.EmitResult, error) {
	if request.Issuer.CityCode == barueriCityCode {
		return ports.EmitResult{
			Status:       domain.StatusRejected,
			ErrorCode:    "E0039",
			ErrorMessage: e0039Message,
			Simulated:    true,
		}, nil
	}
	number := request.Invoice.DPSNumber
	return ports.EmitResult{
		Status:     domain.StatusAuthorized,
		AccessKey:  fmt.Sprintf("SIM%s%015d", request.Issuer.CityCode, number),
		NFSeNumber: fmt.Sprint(number),
		Simulated:  true,
	}, nil
}
