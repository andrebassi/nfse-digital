#!/bin/bash
# Smoke test da API REST contra o binário de verdade: sobe o painel num banco
# temporário (porta 8798), cria uma conta descartável, cadastra emitente,
# tomador e modelo, gera o token PELA TELA (aba API REST) e chama cada rota.
#
# Não emite nota: com conta nova o emissor real abriria o certificado do pass
# e falaria com a SEFIN. A emissão é coberta pelos testes do pacote web com o
# emissor simulado; aqui só entram as recusas que param antes da SEFIN.
#
# Uso: scripts/api-smoke.sh            (precisa de bin/nfse; `task build` antes)
#      KEEP=1 scripts/api-smoke.sh     (deixa o painel no ar para ver no navegador;
#                                       o cookie de sessão fica em $workdir/session)
# Sai rc=0 só se todas as checagens passarem; cada falha soma 1 no rc.
set -uo pipefail

port=8798
base="http://127.0.0.1:$port"
workdir="$(mktemp -d)"
db="$workdir/smoke.db"
jar="$workdir/cookies.txt"
failures=0
if [ "${KEEP:-0}" != 1 ]; then
  trap 'kill "$server_pid" 2>/dev/null; rm -rf "$workdir"' EXIT
fi

# check imprime o resultado de uma verificação e conta as falhas.
check() {
  if [ "$2" = "ok" ]; then echo "✅ $1"; else echo "🛑 $1"; failures=$((failures + 1)); fi
}

# csrf lê o token do formulário de uma página.
csrf() {
  curl --max-time 10 -s -b "$jar" -c "$jar" "$base$1" | grep -o 'name="csrf" value="[^"]*"' | head -1 | cut -d'"' -f4
}

# api chama a API e imprime o corpo seguido do status na última linha; o
# token vem de $api_token.
api() {
  local method=$1 path=$2 body=${3:-}
  curl --max-time 10 -s -w '\n%{http_code}' -X "$method" "$base/api/v1$path" \
    -H "Authorization: Bearer ${api_token:-}" -H "Content-Type: application/json" ${body:+-d "$body"}
}

# status devolve só o código HTTP (última linha da saída de api).
status() { tail -1 <<<"$1"; }

./bin/nfse serve --port "$port" --db "$db" >>"$workdir/serve.log" 2>&1 &
server_pid=$!
for _ in $(seq 1 50); do
  curl --max-time 10 -s -o /dev/null "$base/login" && break
  sleep 0.1
done

password="smoke-$(openssl rand -hex 8)"
token=$(csrf /signup)
curl --max-time 10 -s -o /dev/null -b "$jar" -c "$jar" -X POST "$base/signup" \
  --data-urlencode "csrf=$token" --data-urlencode "name=Dona Exemplo" \
  --data-urlencode "email=dona@exemplo.example" --data-urlencode "password=$password"
# Certificado fictício direto no banco temporário: só abre a trava do painel
# (que exige A1 enviado); nenhum certificado real é lido.
sqlite3 "$db" "INSERT INTO certificate_statuses (account_id, holder_name, cnpj, issuer_name, not_before, not_after, checked_at, portal_authorized_at)
  VALUES (1, 'EXEMPLO TECNOLOGIA LTDA', '11222333000181', 'AC TESTE', '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z', '', '')"

token=$(csrf /settings)
curl --max-time 10 -s -o /dev/null -b "$jar" -c "$jar" -X POST "$base/settings" \
  --data-urlencode "csrf=$token" --data-urlencode "legal_name=EXEMPLO TECNOLOGIA LTDA" \
  --data-urlencode "cnpj=11.222.333/0001-81" --data-urlencode "municipal_registration=000000" \
  --data-urlencode "city_code=3505708" --data-urlencode "city_name=Barueri/SP" \
  --data-urlencode "simples_option=me-epp" --data-urlencode "simples_regime=federal-municipal-sn" \
  --data-urlencode "special_regime=none" --data-urlencode "environment=homologation" \
  --data-urlencode "series=1" --data-urlencode "next_dps_number=1001"
token=$(csrf /customers/new)
curl --max-time 10 -s -o /dev/null -b "$jar" -c "$jar" -X POST "$base/customers" --data-urlencode "csrf=$token" \
  --data-urlencode "name=IPÊ INFORMÁTICA LTDA" --data-urlencode "document=56.789.012/0001-00"
token=$(csrf /templates/new)
curl --max-time 10 -s -o /dev/null -b "$jar" -c "$jar" -X POST "$base/templates" --data-urlencode "csrf=$token" \
  --data-urlencode "name=Suporte mensal" --data-urlencode "description=Suporte técnico - {mes}/{ano}" \
  --data-urlencode "national_tax_code=01.07.01" --data-urlencode "service_city_code=3505708" \
  --data-urlencode "default_amount=1.500,00" --data-urlencode "customer_id=0"

echo "== aba API REST"
page=$(curl --max-time 10 -s -b "$jar" "$base/integrations/api")
check "aba mostra a URL base" "$(grep -q "$base/api/v1</p>" <<<"$page" && echo ok)"
check "exemplo de emissão com o tomador da conta" "$(grep -q 'customer_id&#34;: 1\|"customer_id": 1' <<<"$page" && echo ok)"
token=$(csrf /integrations/api)
page=$(curl --max-time 10 -s -b "$jar" -c "$jar" -X POST "$base/integrations/tokens" \
  --data-urlencode "csrf=$token" --data-urlencode "section=api" --data-urlencode "op=emit")
api_token=$(grep -o 'nf_[A-Za-z0-9_-]\{43\}' <<<"$page" | head -1)
check "gerar token na aba devolve o token" "$([ -n "$api_token" ] && echo ok)"
check "exemplos já com o token" "$(grep -q "Authorization: Bearer $api_token" <<<"$page" && echo ok)"

echo "== rotas autenticadas"
out=$(api GET /account)
check "GET /account 200 em homologação" "$([ "$(status "$out")" = 200 ] && grep -q '"environment": "homologation"' <<<"$out" && echo ok)"
out=$(api GET "/customers?q=ipe")
check "GET /customers acha o tomador" "$([ "$(status "$out")" = 200 ] && grep -q '56789012000100' <<<"$out" && echo ok)"
out=$(api GET /templates)
check "GET /templates com valor padrão 1500.00" "$([ "$(status "$out")" = 200 ] && grep -q '"default_amount": "1500.00"' <<<"$out" && echo ok)"
out=$(api GET "/invoices?limit=5")
check "GET /invoices 200 (lista vazia)" "$([ "$(status "$out")" = 200 ] && grep -q '"invoices": \[\]' <<<"$out" && echo ok)"
check "GET /invoices?limit=0 400" "$([ "$(status "$(api GET "/invoices?limit=0")")" = 400 ] && echo ok)"
check "GET /invoices/by-key desconhecida 404" "$([ "$(status "$(api GET "/invoices/by-key?idempotency_key=x")")" = 404 ] && echo ok)"
check "GET /invoices/999 404" "$([ "$(status "$(api GET /invoices/999)")" = 404 ] && echo ok)"
out=$(api POST /invoices '{"customer_id":1,"template_id":1}')
check "POST /invoices sem chave 422 com fields" "$([ "$(status "$out")" = 422 ] && grep -q '"idempotency_key"' <<<"$out" && echo ok)"
check "POST /invoices com campo desconhecido 400" "$([ "$(status "$(api POST /invoices '{"idempotency_key":"a","confirmProduction":true}')")" = 400 ] && echo ok)"
check "POST /invoices/999/cancel 404" "$([ "$(status "$(api POST /invoices/999/cancel '{"reason":"outros","description":"teste de cancelamento"}')")" = 404 ] && echo ok)"
check "GET /invoices/999/danfse 404" "$([ "$(status "$(api GET /invoices/999/danfse)")" = 404 ] && echo ok)"

echo "== portão"
saved=$api_token
api_token=nf_errado
check "token errado 401" "$([ "$(status "$(api GET /account)")" = 401 ] && echo ok)"
api_token=$saved
code=$(curl --max-time 10 -s -o /dev/null -w '%{http_code}' -H "Host: atacante.example.com" -H "Authorization: Bearer $api_token" "$base/api/v1/account")
check "Host de fora 403" "$([ "$code" = 403 ] && echo ok)"

echo "== públicas"
spec=$(curl --max-time 10 -s "$base/api/v1/openapi.json")
check "openapi.json com servers deste painel" "$(grep -q "\"url\": \"$base/api/v1\"" <<<"$spec" && echo ok)"
paths=$(python3 -c 'import json,sys; d=json.load(sys.stdin); print(sum(len(v) for v in d["paths"].values()))' <<<"$spec")
check "openapi.json descreve 9 operações (medido: $paths)" "$([ "$paths" = 9 ] && echo ok)"
headers=$(curl --max-time 10 -s -D - -o /dev/null "$base/api/docs")
check "/api/docs com CSP só de script local" "$(grep -qi "content-security-policy: .*script-src 'self';" <<<"$headers" && echo ok)"
code=$(curl --max-time 10 -s -o /dev/null -w '%{http_code}' "$base/static/vendor/scalar-api-reference-1.76.4.js")
check "Scalar vendorizado servido (200)" "$([ "$code" = 200 ] && echo ok)"
llms=$(curl --max-time 10 -s "$base/api/llms.txt")
check "llms.txt com URL absoluta" "$(grep -q "$base/api/v1/invoices" <<<"$llms" && echo ok)"

if [ "${KEEP:-0}" = 1 ]; then
  awk '$6 == "session" {print $7}' "$jar" | tail -1 >"$workdir/session"
  echo "painel no ar: $base (pid $server_pid) · sessão em $workdir/session"
fi
echo "falhas: $failures"
exit "$failures"
