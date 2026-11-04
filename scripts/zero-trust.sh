#!/bin/bash
# Põe o painel nfse.bassi.engineer atrás do Cloudflare Access (Zero Trust).
# Idempotente: rodar de novo só confere e corrige.
#
#   - o Access protege só as PORTAS DE ENTRADA do painel: /login e /signup
#     (as duas criam sessão; proteger só /login deixaria o cadastro como
#     desvio). Só ALLOWED_EMAIL passa. Decisão do dono (02/11/2026), que
#     trocou o "tudo menos a doc" por "zero trust aqui -> /login".
#   - o resto do painel não precisa de app próprio: sem sessão, toda página
#     redireciona para /login, que está atrás do Access.
#   - API (/api/v1), MCP (/mcp) e a doc ficam fora do Access, protegidos pelo
#     bearer token — ERP, agente remoto e o playground da doc continuam
#     funcionando.
#
# Passos: [1] DNS com proxy (o Access exige) · [2] SSL Full (strict) SÓ para
# este host, por regra de configuração (a zona tem outros subdomínios) ·
# [3] CNAME _acme-challenge para o Fly renovar o certificado por DNS (com o
# proxy, o desafio por HTTP não chega ao Fly) · [4] aplicações do Access.
#
# Tokens do pass (Cloudflare e Fly), passados pelo stdin ou por variável;
# nunca na linha de comando.
set -euo pipefail
cd "$(dirname "$0")/.."

ZONE=bassi.engineer
HOST=nfse.bassi.engineer
APP=nfse-digital
ALLOWED_EMAIL=andrebassi@gmail.com
PROTECTED_PATHS=("login" "signup")
CF=https://api.cloudflare.com/client/v4
export PATH="$HOME/.local/bin:/usr/bin:/bin:$PATH:/etc/profiles/per-user/$USER/bin"

CLOUDFLARE_API_TOKEN="$(timeout 120s pass show bassi/cloudflare/token)"
FLY_API_TOKEN="$(timeout 120s pass show bassi/flyio/api-token)"
export FLY_API_TOKEN

# cf chama a API do Cloudflare com o token pelo stdin (fora da lista de processos).
cf() {
  printf 'header = "Authorization: Bearer %s"\n' "$CLOUDFLARE_API_TOKEN" |
    timeout 30s curl -sS --max-time 25 -K - -H "Content-Type: application/json" "$@"
}

# ok imprime o resultado de uma chamada (success e errors) e falha se não deu certo.
ok() {
  python3 -c 'import json,sys; d=json.load(sys.stdin); print("  ", sys.argv[1], "ok" if d["success"] else d["errors"]); sys.exit(0 if d["success"] else 1)' "$1"
}

# field lê um campo do JSON da resposta (caminho com pontos).
field() {
  python3 -c 'import json,sys
d=json.load(sys.stdin)
for k in sys.argv[1].split("."):
    d=d[int(k)] if isinstance(d,list) else d[k]
print(d)' "$1"
}

ZONE_ID="$(cf "$CF/zones?name=$ZONE" | field result.0.id)"
ACCOUNT_ID="$(cf "$CF/zones?name=$ZONE" | field result.0.account.id)"

echo "[1/4] DNS $HOST com proxy (laranja)"
RECORD="$(cf "$CF/zones/$ZONE_ID/dns_records?name=$HOST")"
RECORD_ID="$(printf '%s' "$RECORD" | field result.0.id)"
TARGET="$(printf '%s' "$RECORD" | field result.0.content)"
cf -X PATCH "$CF/zones/$ZONE_ID/dns_records/$RECORD_ID" --data '{"proxied":true}' | ok "proxy em $HOST → $TARGET"

echo "[2/4] SSL Full (strict) só para $HOST (regra de configuração)"
# O ruleset de configuração da zona é um só (fase http_config_settings);
# a regra deste host é acrescentada sem tocar nas outras.
RULESET="$(cf "$CF/zones/$ZONE_ID/rulesets/phases/http_config_settings/entrypoint")"
if printf '%s' "$RULESET" | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if d.get("success") else 1)'; then
  RULESET_ID="$(printf '%s' "$RULESET" | field result.id)"
  if printf '%s' "$RULESET" | grep -q "nfse-ssl-strict"; then
    echo "   regra já existe"
  else
    cf -X POST "$CF/zones/$ZONE_ID/rulesets/$RULESET_ID/rules" --data "{\"description\":\"nfse-ssl-strict\",\"expression\":\"(http.host eq \\\"$HOST\\\")\",\"action\":\"set_config\",\"action_parameters\":{\"ssl\":\"strict\"}}" | ok "regra SSL strict"
  fi
else
  cf -X PUT "$CF/zones/$ZONE_ID/rulesets/phases/http_config_settings/entrypoint" --data "{\"rules\":[{\"description\":\"nfse-ssl-strict\",\"expression\":\"(http.host eq \\\"$HOST\\\")\",\"action\":\"set_config\",\"action_parameters\":{\"ssl\":\"strict\"}}]}" | ok "ruleset com a regra SSL strict"
fi

echo "[3/4] _acme-challenge para o Fly renovar o certificado por DNS"
ACME_TARGET="$(timeout 60s fly certs show "$HOST" -a "$APP" --json | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("DNSValidationTarget") or d.get("dnsValidationTarget") or "")')"
if [ -n "$ACME_TARGET" ]; then
  ACME_NAME="_acme-challenge.$HOST"
  ACME_ID="$(cf "$CF/zones/$ZONE_ID/dns_records?name=$ACME_NAME" | python3 -c 'import json,sys; r=json.load(sys.stdin)["result"]; print(r[0]["id"] if r else "")')"
  BODY="{\"type\":\"CNAME\",\"name\":\"$ACME_NAME\",\"content\":\"$ACME_TARGET\",\"proxied\":false,\"ttl\":300}"
  if [ -z "$ACME_ID" ]; then
    cf -X POST "$CF/zones/$ZONE_ID/dns_records" --data "$BODY" | ok "CNAME $ACME_NAME → $ACME_TARGET"
  else
    cf -X PUT "$CF/zones/$ZONE_ID/dns_records/$ACME_ID" --data "$BODY" | ok "CNAME $ACME_NAME → $ACME_TARGET"
  fi
else
  echo "   🛑 o Fly não informou o alvo de validação por DNS; renovação do certificado precisa ser conferida"
fi

echo "[4/4] aplicações do Access"
APPS="$(cf "$CF/accounts/$ACCOUNT_ID/access/apps?per_page=100")"
printf '%s' "$APPS" | ok "leitura das aplicações (permissão do token)" >/dev/null

# app_id devolve o id da aplicação com o domínio dado (vazio se não existe).
app_id() {
  printf '%s' "$APPS" | python3 -c 'import json,sys
for a in json.load(sys.stdin)["result"]:
    if a.get("domain") == sys.argv[1]:
        print(a["id"]); break' "$1"
}

# ensure_app cria (ou reaproveita) a aplicação do domínio e garante a
# política: allow só o e-mail do dono (bypass fica para uso futuro).
ensure_app() {
  local domain=$1 name=$2 decision=$3 include id
  id="$(app_id "$domain")"
  if [ -z "$id" ]; then
    id="$(cf -X POST "$CF/accounts/$ACCOUNT_ID/access/apps" --data "{\"name\":\"$name\",\"domain\":\"$domain\",\"type\":\"self_hosted\",\"session_duration\":\"24h\",\"app_launcher_visible\":false}" | field result.id)"
    echo "   aplicação criada: $name ($domain)"
  else
    echo "   aplicação existe: $name ($domain)"
  fi
  if [ "$decision" = allow ]; then
    include="[{\"email\":{\"email\":\"$ALLOWED_EMAIL\"}}]"
  else
    include='[{"everyone":{}}]'
  fi
  if cf "$CF/accounts/$ACCOUNT_ID/access/apps/$id/policies" | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)["result"] else 1)'; then
    echo "   política já existe"
  else
    cf -X POST "$CF/accounts/$ACCOUNT_ID/access/apps/$id/policies" --data "{\"name\":\"$name - $decision\",\"decision\":\"$decision\",\"include\":$include,\"precedence\":1}" | ok "política $decision"
  fi
}

for path in "${PROTECTED_PATHS[@]}"; do
  ensure_app "$HOST/$path" "nfse painel /$path" allow
done
unset CLOUDFLARE_API_TOKEN FLY_API_TOKEN

echo "== conferência (pelo lado de fora)"
sleep 5
# esperado: /login e /signup → 302 para o cloudflareaccess.com; páginas do
# painel → 303 para /login; API sem token → 401; doc → 200.
for path in "login" "signup" "" "invoices" "api/v1/account" "api/docs" "api/v1/openapi.json" "static/app.css"; do
  code=$(timeout 20s curl -s -o /dev/null -w '%{http_code} %{redirect_url}' "https://$HOST/$path")
  echo "  /$path → $code"
done
