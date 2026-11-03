#!/bin/bash
# Regera os três SDKs a partir do contrato (internal/adapters/web/api/openapi.json),
# com versões fixas dos geradores — o mesmo contrato sempre gera o mesmo código:
#   Go         oapi-codegen v2.5.1        → sdk/go/client.gen.go
#   Python     openapi-python-client 0.28.2 → sdk/python/ (a pasta é recriada inteira)
#   TypeScript @hey-api/openapi-ts 0.99.0 → sdk/typescript/src/
# Antes de gerar, valida o contrato com o CLI do Scalar.
#
# Uso: scripts/sdk-generate.sh          (regera)
#      scripts/sdk-generate.sh --check  (regera e reprova se o SDK versionado
#                                        estiver diferente do que o contrato gera)
set -euo pipefail
cd "$(dirname "$0")/.."
spec="internal/adapters/web/api/openapi.json"

echo "[1/4] contrato (Scalar CLI 2.13.2)"
# O CLI roda numa pasta vazia: o npx não deve achar package.json do projeto.
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
(cd "$scratch" && nix shell nixpkgs#nodejs_24 --command npx -y @scalar/cli@2.13.2 document validate "$OLDPWD/$spec" 2>&1 | grep -v "npm warn")

echo "[2/4] Go"
(cd sdk/go && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1 -config oapi-codegen.yaml "../../$spec" 2>&1 \
  | grep -v "OpenAPI 3.1.x specification" || true)

echo "[3/4] Python"
nix shell nixpkgs#uv --command uvx openapi-python-client@0.28.2 generate --path "$spec" --config sdk/python-config.yaml \
  --output-path sdk/python --overwrite --meta uv 2>&1 | grep -E "^(Generating|Error)" || true

echo "[4/4] TypeScript"
(cd sdk/typescript && nix shell nixpkgs#nodejs_24 --command sh -c \
  "npm ci --silent --no-fund --no-audit && npx openapi-ts -f openapi-ts.config.json >/dev/null && npm run --silent typecheck")

if [ "${1:-}" = "--check" ]; then
  # Arquivo não versionado também é diferença: --exit-code sozinho não vê arquivo novo.
  if ! git diff --quiet -- sdk/ || [ -n "$(git ls-files --others --exclude-standard -- sdk/)" ]; then
    echo "🛑 o SDK versionado não bate com o contrato: rode task sdk:generate e versione"
    git status --short -- sdk/
    exit 1
  fi
  echo "✅ SDKs em dia com o contrato"
fi
