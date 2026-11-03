#!/bin/bash
# Verificação ponta a ponta da persistência em SQLite: sobe o painel num banco
# temporário, cria a conta e o emitente (dados fictícios), REINICIA o processo,
# entra de novo e confere que conta, emitente e a nota emitida continuam lá.
#
# Uso: scripts/e2e-sqlite.sh   (precisa de bin/nfse; rode `task build` antes)
# Sai rc=0 só se todas as checagens passarem; cada falha soma 1 no rc.
set -uo pipefail

port=8799
base="http://127.0.0.1:$port"
workdir="$(mktemp -d)"
db="$workdir/e2e.db"
jar="$workdir/cookies.txt"
failures=0
trap 'kill "$server_pid" 2>/dev/null; rm -rf "$workdir"' EXIT

# start sobe o painel no banco temporário e espera a porta responder.
start() {
  ./bin/nfse serve --port "$port" --db "$db" >>"$workdir/serve.log" 2>&1 &
  server_pid=$!
  for _ in $(seq 1 50); do
    curl --max-time 10 -s -o /dev/null "$base/login" && return 0
    sleep 0.1
  done
  echo "🛑 painel não subiu"; cat "$workdir/serve.log"; exit 1
}

# csrf lê o token do formulário de uma página.
csrf() {
  curl --max-time 10 -s -b "$jar" -c "$jar" "$base$1" | grep -o 'name="csrf" value="[^"]*"' | head -1 | cut -d'"' -f4
}

# check imprime o resultado de uma verificação e conta as falhas.
check() {
  if [ "$2" = "ok" ]; then echo "✅ $1"; else echo "🛑 $1"; failures=$((failures + 1)); fi
}

password="e2e-$(openssl rand -hex 8)"

echo "== 1ª execução: banco vazio"
start
token=$(csrf /signup)
code=$(curl --max-time 10 -s -o /dev/null -w '%{http_code}' -b "$jar" -c "$jar" -X POST "$base/signup" \
  --data-urlencode "csrf=$token" --data-urlencode "name=Dona Exemplo" \
  --data-urlencode "email=dona@exemplo.example" --data-urlencode "password=$password")
check "criar conta redireciona (303)" "$([ "$code" = 303 ] && echo ok)"

token=$(csrf /settings)
code=$(curl --max-time 10 -s -o /dev/null -w '%{http_code}' -b "$jar" -c "$jar" -X POST "$base/settings" \
  --data-urlencode "csrf=$token" --data-urlencode "legal_name=EXEMPLO TECNOLOGIA LTDA" \
  --data-urlencode "cnpj=11.222.333/0001-81" --data-urlencode "municipal_registration=000000" \
  --data-urlencode "city_code=3505708" --data-urlencode "city_name=Barueri/SP" \
  --data-urlencode "simples_option=me-epp" --data-urlencode "simples_regime=federal-municipal-sn" \
  --data-urlencode "special_regime=none" --data-urlencode "environment=homologation" \
  --data-urlencode "series=1" --data-urlencode "next_dps_number=1001")
check "salvar emitente redireciona (303)" "$([ "$code" = 303 ] && echo ok)"

token=$(csrf /customers/new)
curl --max-time 10 -s -o /dev/null -b "$jar" -c "$jar" -X POST "$base/customers" --data-urlencode "csrf=$token" \
  --data-urlencode "name=IPÊ INFORMÁTICA LTDA" --data-urlencode "document=56.789.012/0001-00"
token=$(csrf /templates/new)
curl --max-time 10 -s -o /dev/null -b "$jar" -c "$jar" -X POST "$base/templates" --data-urlencode "csrf=$token" \
  --data-urlencode "name=Ipê mensal" --data-urlencode "description=Serviço de {mes}/{ano}" \
  --data-urlencode "national_tax_code=01.07.01" --data-urlencode "service_city_code=3505708" \
  --data-urlencode "default_amount=19.000,00" --data-urlencode "customer_id=0"
token=$(csrf /emit)
code=$(curl --max-time 10 -s -o /dev/null -w '%{http_code}' -b "$jar" -c "$jar" -X POST "$base/emit" --data-urlencode "csrf=$token" \
  --data-urlencode "action=emit" --data-urlencode "customer_id=1" --data-urlencode "template_id=1" \
  --data-urlencode "amount=19.000,00" --data-urlencode "competence=2026-10-29" --data-urlencode "description=teste e2e")
check "emitir (simulado) redireciona (303)" "$([ "$code" = 303 ] && echo ok)"

echo "== reinício do processo"
kill "$server_pid"; wait "$server_pid" 2>/dev/null
rm -f "$jar"
start

token=$(csrf /login)
code=$(curl --max-time 10 -s -o /dev/null -w '%{http_code}' -b "$jar" -c "$jar" -X POST "$base/login" \
  --data-urlencode "csrf=$token" --data-urlencode "email=dona@exemplo.example" --data-urlencode "password=$password")
check "entrar com a conta gravada (303)" "$([ "$code" = 303 ] && echo ok)"

body=$(curl --max-time 10 -s -b "$jar" "$base/settings")
check "emitente sobreviveu ao reinício" "$(grep -q '11.222.333/0001-81' <<<"$body" && echo ok)"
body=$(curl --max-time 10 -s -b "$jar" "$base/invoices?period=90")
check "nota sobreviveu, rejeitada com E0039 simulado" "$(grep -q 'rejeitada' <<<"$body" && grep -q 'IPÊ INFORMÁTICA LTDA' <<<"$body" && echo ok)"
body=$(curl --max-time 10 -s -b "$jar" "$base/settings")
check "numeração avançou para 1002" "$(grep -q 'value="1002"' <<<"$body" && echo ok)"
perm=$(stat -f '%Lp' "$db" 2>/dev/null || stat -c '%a' "$db")
check "arquivo do banco com permissão 600 ($perm)" "$([ "$perm" = 600 ] && echo ok)"

echo "falhas: $failures"
exit "$failures"
