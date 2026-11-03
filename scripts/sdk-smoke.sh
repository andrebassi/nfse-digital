#!/bin/bash
# Smoke dos três SDKs (Go, Python, TypeScript) contra o binário de verdade:
# sobe o painel num banco temporário (porta 8796), cria conta, emitente,
# tomador e modelo, gera o token PELA TELA e roda o mesmo roteiro em cada SDK
# (sdk/go/smoke, sdk/python_smoke.py, sdk/typescript/smoke.ts). Não emite nota:
# emitir de verdade falaria com a SEFIN.
#
# Uso: scripts/sdk-smoke.sh   (precisa de bin/nfse; `task build` antes)
# Sai rc=0 só se os três SDKs passarem; cada SDK que falha soma 1 no rc.
set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
port=8796
base="http://127.0.0.1:$port"
workdir="$(mktemp -d)"
db="$workdir/smoke.db"
jar="$workdir/cookies.txt"
failures=0
trap 'kill "$server_pid" 2>/dev/null; rm -rf "$workdir"' EXIT

# csrf lê o token do formulário de uma página.
csrf() {
  curl --max-time 10 -s -b "$jar" -c "$jar" "$base$1" | grep -o 'name="csrf" value="[^"]*"' | head -1 | cut -d'"' -f4
}

# form manda um POST de formulário do painel com o CSRF da página dada.
form() {
  local page=$1 action=$2
  shift 2
  local token
  token=$(csrf "$page")
  curl --max-time 10 -s -o /dev/null -b "$jar" -c "$jar" -X POST "$base$action" --data-urlencode "csrf=$token" "$@"
}

cd "$root"
HOME="$workdir" NFSE_CERT_KEY="" ./bin/nfse serve --port "$port" --db "$db" >>"$workdir/serve.log" 2>&1 &
server_pid=$!
for _ in $(seq 1 50); do
  curl --max-time 2 -s -o /dev/null "$base/login" && break
  sleep 0.1
done

form /signup /signup --data-urlencode "name=Dona Exemplo" --data-urlencode "email=dona@exemplo.example" \
  --data-urlencode "password=smoke-$(openssl rand -hex 8)"
# Certificado fictício só para abrir a trava do painel; nenhum A1 é lido.
sqlite3 "$db" "INSERT INTO certificate_statuses (account_id, holder_name, cnpj, issuer_name, not_before, not_after, checked_at, portal_authorized_at)
  VALUES (1, 'EXEMPLO TECNOLOGIA LTDA', '11222333000181', 'AC TESTE', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z', '', '')"
form /settings /settings --data-urlencode "legal_name=EXEMPLO TECNOLOGIA LTDA" --data-urlencode "cnpj=11.222.333/0001-81" \
  --data-urlencode "municipal_registration=000000" --data-urlencode "city_code=3505708" --data-urlencode "city_name=Barueri/SP" \
  --data-urlencode "simples_option=me-epp" --data-urlencode "simples_regime=federal-municipal-sn" --data-urlencode "special_regime=none" \
  --data-urlencode "environment=homologation" --data-urlencode "series=1" --data-urlencode "next_dps_number=1001"
form /customers/new /customers --data-urlencode "name=IPÊ INFORMÁTICA LTDA" --data-urlencode "document=56.789.012/0001-00"
form /templates/new /templates --data-urlencode "name=Suporte mensal" --data-urlencode "description=Suporte - {mes}/{ano}" \
  --data-urlencode "national_tax_code=01.07.01" --data-urlencode "service_city_code=3505708" --data-urlencode "default_amount=1.500,00"
token=$(csrf /integrations/api)
NFSE_TOKEN=$(curl --max-time 10 -s -b "$jar" -c "$jar" -X POST "$base/integrations/tokens" --data-urlencode "csrf=$token" \
  --data-urlencode "section=api" | grep -o 'nf_[A-Za-z0-9_-]\{43\}' | head -1)
[ -n "$NFSE_TOKEN" ] || { echo "🛑 o painel não gerou o token"; cat "$workdir/serve.log"; exit 1; }
export NFSE_URL="$base/api/v1" NFSE_TOKEN

echo "== Go"
(cd sdk/go && go run ./smoke) || failures=$((failures + 1))

echo "== Python"
(cd sdk && nix shell nixpkgs#uv --command uv run --quiet --with ./python python python_smoke.py) || failures=$((failures + 1))

echo "== TypeScript"
# Compila o roteiro junto com o SDK gerado (tsc e tipos do Node do próprio
# pacote, pelo package-lock) e roda no Node 24 (ESM).
(cd sdk/typescript && nix shell nixpkgs#nodejs_24 --command sh -c "
  npm ci --silent --no-fund --no-audit && npx tsc --outDir '$workdir/ts' --module nodenext --moduleResolution nodenext \
    --target es2022 --strict --skipLibCheck --types node smoke.ts && node '$workdir/ts/smoke.js'") || failures=$((failures + 1))

echo "SDKs com falha: $failures"
exit "$failures"
