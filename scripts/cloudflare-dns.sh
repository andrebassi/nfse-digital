#!/bin/bash
# Cria (ou corrige) o CNAME nfse.bassi.engineer → nfse-digital.fly.dev na zona
# do Cloudflare. SEM proxy (nuvem cinza): o certificado é emitido pelo Fly, e o
# proxy laranja esconderia o CNAME do desafio ACME do Fly. Token em
# pass bassi/cloudflare/token, só no header — nunca na URL.
set -euo pipefail

ZONE=bassi.engineer
NAME=nfse.bassi.engineer
TARGET=nfse-digital.fly.dev
API=https://api.cloudflare.com/client/v4
export PATH="$HOME/.local/bin:/usr/bin:/bin:$PATH:/etc/profiles/per-user/$USER/bin"

TOKEN="$(timeout 120s pass show bassi/cloudflare/token)"
# cf faz a chamada autenticada; o token sai pelo header de um arquivo de
# config do curl lido do stdin, para não aparecer na lista de processos.
cf() {
  printf 'header = "Authorization: Bearer %s"\n' "$TOKEN" |
    timeout 30s curl -sS --max-time 25 -K - -H "Content-Type: application/json" "$@"
}

ZONE_ID="$(cf "$API/zones?name=$ZONE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"][0]["id"])')"
RECORD="$(cf "$API/zones/$ZONE_ID/dns_records?name=$NAME")"
RECORD_ID="$(printf '%s' "$RECORD" | python3 -c 'import json,sys; r=json.load(sys.stdin)["result"]; print(r[0]["id"] if r else "")')"
BODY="{\"type\":\"CNAME\",\"name\":\"$NAME\",\"content\":\"$TARGET\",\"proxied\":false,\"ttl\":300}"

if [ -z "$RECORD_ID" ]; then
  cf -X POST "$API/zones/$ZONE_ID/dns_records" --data "$BODY" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("criado:", d["success"], d["errors"])'
else
  cf -X PUT "$API/zones/$ZONE_ID/dns_records/$RECORD_ID" --data "$BODY" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("atualizado:", d["success"], d["errors"])'
fi
# Conferência pelo DNS público, não pela API: é o que o navegador e o ACME veem.
timeout 15s dig +short CNAME "$NAME" @1.1.1.1
