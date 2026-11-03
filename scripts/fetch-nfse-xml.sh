#!/bin/bash
# Baixa da SEFIN a NFS-e de uma chave de acesso e grava o XML legível.
#
# Uso: scripts/fetch-nfse-xml.sh <chave-50-dígitos> [--production] [saída.xml]
#
# Serve para comparar o que OUTRO emissor mandou e foi autorizado (a NFS-e
# devolvida carrega a DPS original dentro) com o XML que o nfse gera. A SEFIN
# responde {"nfseXmlGZipB64": …}; o script tira o gzip+base64 e indenta.
# Lê com o A1 do pass (aprovação no celular ou YubiKey), só leitura.
set -euo pipefail
cd "$(dirname "$0")/.."

key="${1:?uso: $0 <chave> [--production] [saída.xml]}"
shift
env_flag=""
if [ "${1:-}" = "--production" ]; then env_flag="--production"; shift; fi
out="${1:-/tmp/nfse-$key.xml}"

# A chave de acesso tem 50 dígitos (TSChaveAcesso): rejeitar cedo evita uma
# ida à SEFIN que só devolveria 400.
[[ "$key" =~ ^[0-9]{50}$ ]] || { echo "🛑 chave precisa de 50 dígitos"; exit 1; }

# O pass precisa achar o gpg2 roteador antes do gpg do Nix (skill pass-route).
export PATH="$HOME/.local/bin:/usr/bin:/bin:$PATH"
[ -x bin/nfse ] || task build

# limit alto: o corpo inteiro precisa chegar para o decode; o cabeçalho
# "GET …" e "HTTP …" ocupa as duas primeiras linhas da saída do fetch.
raw=$(./bin/nfse fetch $env_flag --limit 10000000 "/nfse/$key")
echo "$raw" | sed -n 2p
echo "$raw" | sed -n '3,$p' \
  | python3 -c 'import sys,json,base64,gzip; print(gzip.decompress(base64.b64decode(json.load(sys.stdin)["nfseXmlGZipB64"])).decode())' \
  | python3 -c 'import sys,xml.dom.minidom as m; print(m.parseString(sys.stdin.read()).toprettyxml(indent="  "))' \
  > "$out"
echo "XML gravado em $out ($(wc -c < "$out" | tr -d ' ') bytes)"
