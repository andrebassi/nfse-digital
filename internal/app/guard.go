package app

import (
	"errors"

	"nfse/internal/domain"
)

// ErrProductionNotConfirmed recusa operação em produção sem a confirmação do
// canal: --production na CLI, confirm_production no MCP e no painel, ou a
// permissão gravada no agendamento.
var ErrProductionNotConfirmed = errors.New("o emitente está em produção (nota com valor fiscal): confirme explicitamente para seguir")

// EnsureEnvironment é a regra única da guarda de produção (story 4.1): em
// homologação passa sempre; em produção só com confirmação. Todo caminho que
// fala com a SEFIN em nome do dono — emitir, reenviar, cancelar, agendar —
// passa por aqui antes de reservar número ou abrir o certificado.
func EnsureEnvironment(environment domain.Environment, confirmed bool) error {
	if environment == domain.EnvProduction && !confirmed {
		return ErrProductionNotConfirmed
	}
	return nil
}
