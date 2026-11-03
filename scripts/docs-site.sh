#!/bin/bash
# Site de documentação (Fumadocs, em site/) — nfse-docs.bassi.engineer.
#
#   scripts/docs-site.sh build    contrato em public/ + export estático em site/out
#   scripts/docs-site.sh dev      servidor de desenvolvimento em http://localhost:3000
#   scripts/docs-site.sh deploy   build + Cloudflare Pages (projeto nfse-docs) + domínio
#
# O contrato é o MESMO do painel (internal/adapters/web/api/openapi.json):
# copiado para site/public/openapi.json com o servers apontando para a
# produção, para a Referência (Scalar) e o "Download OpenAPI" do site.
# Node vem do Nix (nodejs_24), nunca do sistema.
set -euo pipefail
cd "$(dirname "$0")/.."

PROJECT=nfse-docs
DOMAIN=nfse-docs.bassi.engineer
ZONE=bassi.engineer
# NFSE_DOCS_API_URL troca o servidor do contrato (teste local do playground).
API_URL="${NFSE_DOCS_API_URL:-https://nfse.bassi.engineer/api/v1}"
CF=https://api.cloudflare.com/client/v4

# contract copia o contrato trocando o servers pela produção.
contract() {
  python3 - "$API_URL" <<'EOF'
import json, sys
spec = json.load(open("internal/adapters/web/api/openapi.json"))
spec["servers"] = [{"url": sys.argv[1], "description": "produção"}]
json.dump(spec, open("site/public/openapi.json", "w"), ensure_ascii=False, indent=2)
EOF
}

# node roda um comando dentro de site/ com o Node 24 do Nix.
node_run() {
  (cd site && nix shell nixpkgs#nodejs_24 --command sh -c "$1")
}

build() {
  echo "[build 1/3] contrato → site/public/openapi.json ($API_URL)"
  contract
  echo "[build 2/3] páginas de operação geradas do contrato (content/docs/api/endpoints)"
  node_run "npm ci --silent --no-fund --no-audit && rm -rf content/docs/api/endpoints && node scripts/generate-api-pages.ts"
  echo "[build 3/3] next build (export estático em site/out)"
  node_run "npm run build"
  echo "páginas geradas: $(find site/out -name '*.html' | wc -l | tr -d ' ')"
}

# cf chama a API do Cloudflare com o token do pass pelo stdin (nunca na
# linha de comando nem na URL).
cf() {
  printf 'header = "Authorization: Bearer %s"\n' "$CLOUDFLARE_API_TOKEN" |
    timeout 30s curl -sS --max-time 25 -K - -H "Content-Type: application/json" "$@"
}

deploy() {
  export PATH="$HOME/.local/bin:/usr/bin:/bin:$PATH:/etc/profiles/per-user/$USER/bin"
  CLOUDFLARE_API_TOKEN="$(timeout 120s pass show bassi/cloudflare/token)"
  export CLOUDFLARE_API_TOKEN
  build
  echo "[deploy 1/4] conta e zona do Cloudflare"
  CLOUDFLARE_ACCOUNT_ID="$(cf "$CF/zones?name=$ZONE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"][0]["account"]["id"])')"
  ZONE_ID="$(cf "$CF/zones?name=$ZONE" | python3 -c 'import json,sys; print(json.load(sys.stdin)["result"][0]["id"])')"
  export CLOUDFLARE_ACCOUNT_ID
  echo "[deploy 2/4] projeto Pages $PROJECT (cria se não existe)"
  if ! cf "$CF/accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects/$PROJECT" | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)["success"] else 1)'; then
    cf -X POST "$CF/accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects" \
      --data "{\"name\":\"$PROJECT\",\"production_branch\":\"main\"}" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("  criado:", d["success"], d["errors"])'
  fi
  echo "[deploy 3/4] upload de site/out (wrangler)"
  node_run "npx -y wrangler@4.156.2 pages deploy out --project-name $PROJECT --branch main --commit-dirty=true"
  echo "[deploy 4/4] domínio $DOMAIN (Pages + CNAME)"
  cf -X POST "$CF/accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects/$PROJECT/domains" --data "{\"name\":\"$DOMAIN\"}" |
    python3 -c 'import json,sys; d=json.load(sys.stdin); print("  domínio no Pages:", d["success"] or d["errors"])'
  RECORD_ID="$(cf "$CF/zones/$ZONE_ID/dns_records?name=$DOMAIN" | python3 -c 'import json,sys; r=json.load(sys.stdin)["result"]; print(r[0]["id"] if r else "")')"
  BODY="{\"type\":\"CNAME\",\"name\":\"$DOMAIN\",\"content\":\"$PROJECT.pages.dev\",\"proxied\":true,\"ttl\":1}"
  if [ -z "$RECORD_ID" ]; then
    cf -X POST "$CF/zones/$ZONE_ID/dns_records" --data "$BODY" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("  CNAME criado:", d["success"], d["errors"])'
  else
    cf -X PUT "$CF/zones/$ZONE_ID/dns_records/$RECORD_ID" --data "$BODY" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("  CNAME atualizado:", d["success"], d["errors"])'
  fi
  unset CLOUDFLARE_API_TOKEN
}

case "${1:-build}" in
  build) build ;;
  dev) contract && node_run "npm run dev" ;;
  deploy) deploy ;;
  *) echo "uso: $0 build|dev|deploy"; exit 2 ;;
esac
