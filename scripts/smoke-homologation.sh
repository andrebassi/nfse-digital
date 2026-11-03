#!/bin/bash
# Smoke test REAL na homologação (produção restrita) da SEFIN Nacional: monta
# um banco temporário, tira o emitente do próprio certificado A1 do pass,
# cadastra um tomador e um modelo, e emite R$ 1,00 com `nfse emit`.
#
# Nada aqui tem valor fiscal: a homologação não gera nota válida, e o
# emissor recusa produção (story 4.1).
#
# Uso:
#   NFSE_SMOKE_TAKER=<CNPJ do tomador> scripts/smoke-homologation.sh
# Opcionais: NFSE_SMOKE_IM (inscrição municipal; a homologação de Barueri exige,
# E0116 medido em 30/10/2026), NFSE_SMOKE_TAKER_NAME, NFSE_SMOKE_CITY (IBGE do emitente,
# padrão 3505708), NFSE_SMOKE_DPS (número da DPS, padrão 300001).
#
# Resultado esperado para emitente de Barueri: recusa cadastral E0039 ou E0116 (o
# cadastro de teste do município não tem o emitente), depois de mTLS, esquema e
# assinatura aceitos. Qualquer outro
# código aponta defeito no XML, na assinatura ou no cadastro — sai rc=1.
set -uo pipefail

: "${NFSE_SMOKE_TAKER:?informe o CNPJ do tomador em NFSE_SMOKE_TAKER}"
taker_name="${NFSE_SMOKE_TAKER_NAME:-TOMADOR DE TESTE DE HOMOLOGACAO}"
city="${NFSE_SMOKE_CITY:-3505708}"
dps_number="${NFSE_SMOKE_DPS:-300001}"
port=8797
base="http://127.0.0.1:$port"
workdir="$(mktemp -d)"
db="$workdir/smoke.db"
jar="$workdir/cookies.txt"
trap 'kill "$server_pid" 2>/dev/null; rm -rf "$workdir"' EXIT

# O pass do dono decifra pelo roteador gpg2 em ~/.local/bin (celular ou YubiKey).
export PATH="$HOME/.local/bin:/usr/bin:/bin:$PATH"

echo "== 1/4 certificado (aprovação no celular ou YubiKey)"
cert_output=$(./bin/nfse cert check) || { echo "🛑 não li o certificado"; exit 1; }
echo "$cert_output"
legal_name=$(sed -n 's/^titular: *//p' <<<"$cert_output")
cnpj=$(sed -n 's/^CNPJ: *//p' <<<"$cert_output")

echo "== 2/4 banco temporário com emitente, tomador e modelo"
./bin/nfse serve --port "$port" --db "$db" >"$workdir/serve.log" 2>&1 &
server_pid=$!
for _ in $(seq 1 50); do curl --max-time 2 -s -o /dev/null "$base/login" && break; sleep 0.1; done

# csrf lê o token do formulário de uma página.
csrf() { curl --max-time 10 -s -b "$jar" -c "$jar" "$base$1" | grep -o 'name="csrf" value="[^"]*"' | head -1 | cut -d'"' -f4; }

# post envia um formulário e devolve o código HTTP.
post() {
  local path="$1"; shift
  local token; token=$(csrf "$1"); shift
  curl --max-time 10 -s -o /dev/null -w '%{http_code}' -b "$jar" -c "$jar" -X POST "$base$path" --data-urlencode "csrf=$token" "$@"
}

codes=""
codes+=$(post /signup /signup --data-urlencode "name=Smoke" --data-urlencode "email=smoke@exemplo.example" \
  --data-urlencode "password=smoke-$(openssl rand -hex 8)")
# IM vem de NFSE_SMOKE_IM: a homologação de Barueri exige (E0116 em 30/10/2026).
codes+=" "$(post /settings /settings --data-urlencode "legal_name=$legal_name" --data-urlencode "cnpj=$cnpj" \
  --data-urlencode "municipal_registration=${NFSE_SMOKE_IM:-}" --data-urlencode "city_code=$city" --data-urlencode "city_name=emitente" \
  --data-urlencode "simples_option=me-epp" --data-urlencode "simples_regime=federal-municipal-sn" \
  --data-urlencode "special_regime=none" --data-urlencode "environment=homologation" \
  --data-urlencode "series=1" --data-urlencode "next_dps_number=$dps_number")
codes+=" "$(post /customers /customers/new --data-urlencode "name=$taker_name" --data-urlencode "document=$NFSE_SMOKE_TAKER")
codes+=" "$(post /templates /templates/new --data-urlencode "name=Smoke" \
  --data-urlencode "description=Teste de integração em homologação - sem valor fiscal" \
  --data-urlencode "national_tax_code=010701" --data-urlencode "municipal_tax_code=220" --data-urlencode "nbs=115013000" \
  --data-urlencode "service_city_code=$city" --data-urlencode "default_amount=1,00" --data-urlencode "customer_id=0")
kill "$server_pid"; wait "$server_pid" 2>/dev/null
[ "$codes" = "303 303 303 303" ] || { echo "🛑 cadastro falhou: $codes"; cat "$workdir/serve.log"; exit 1; }
echo "✅ cadastro: $codes"

echo "== 3/4 emissão na homologação da SEFIN (aprovação no celular de novo, se o cache venceu)"
emit_output=$(./bin/nfse emit --db "$db" --customer "$NFSE_SMOKE_TAKER" --template Smoke --show-xml 2>&1)
emit_rc=$?
echo "prestador enviado: $(grep -o '<prest>.*</prest>' <<<"$emit_output")"
echo "$emit_output" | sed -n '/--- resposta crua da SEFIN ---/,$p'

echo "== 4/4 resultado"
# Critério medido em 30/10/2026: na produção restrita o emitente de Barueri não
# está no cadastro do município (CNC), então a melhor resposta possível é uma
# recusa CADASTRAL — E0039 (município sem emissor nacional) ou E0116 (IM não
# confere com o CNC). Ela só chega depois de mTLS, esquema e assinatura
# aceitos. Recusa de esquema (E1235), de assinatura ou de transporte reprova.
if grep -qE "\[(E0039|E0116)\]" <<<"$emit_output"; then
  echo "✅ recusa cadastral do ambiente de teste: mTLS, esquema e assinatura aceitos pela SEFIN"
  exit 0
fi
if grep -q "chave:" <<<"$emit_output"; then
  echo "✅ autorizada na homologação"
  exit 0
fi
echo "🛑 resultado diferente do esperado (rc=$emit_rc) — ver a resposta crua acima"
exit 1
