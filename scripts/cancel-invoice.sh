#!/bin/bash
# Cancela na SEFIN uma nota do painel publicado (banco no Turso) pela CLI.
#
# Uso: scripts/cancel-invoice.sh <id-da-nota> [--production] [motivo] [justificativa]
#   motivo: erro_emissao (padrão), servico_nao_prestado ou outros
#
# O painel ainda não tem botão de cancelar; a CLI fala com o MESMO banco do
# Fly (URL do Turso no fly.toml) e assina o evento com o A1 do pass — YubiKey
# ou aprovação no celular. --production é a confirmação explícita da guarda
# (story 4.1): sem ela, nota de produção é recusada antes de ir à SEFIN.
set -euo pipefail
cd "$(dirname "$0")/.."

id="${1:?uso: $0 <id> [--production] [motivo] [justificativa]}"
shift
env_flag=""
if [ "${1:-}" = "--production" ]; then env_flag="--production"; shift; fi
reason="${1:-erro_emissao}"
description="${2:-Nota de teste do emissor nfse-digital emitida por engano; cancelada sem prestação de serviço.}"

# O pass precisa achar o gpg2 roteador antes do gpg do Nix (skill pass-route).
export PATH="$HOME/.local/bin:/usr/bin:/bin:$PATH"
[ -x bin/nfse ] || task build

# Mesmo banco do painel publicado: a URL sai do fly.toml, que é a fonte dela.
db=$(sed -n 's/^ *NFSE_DB *= *"\(.*\)"/\1/p' fly.toml)
[ -n "$db" ] || { echo "🛑 NFSE_DB não encontrado no fly.toml"; exit 1; }
TURSO_AUTH_TOKEN="$(pass show bassi/turso/nfse-auth-token)"
export TURSO_AUTH_TOKEN
# A conta do dono; a CLI escolhe a primeira conta quando a variável falta.
export NFSE_ACCOUNT="${NFSE_ACCOUNT:-andrebassi@gmail.com}"

./bin/nfse cancel --db "$db" --reason "$reason" --description "$description" $env_flag --show-xml "$id"
