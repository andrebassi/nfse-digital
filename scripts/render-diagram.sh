#!/bin/bash
# Regenera o diagrama de arquitetura (docs/diagram): monta o HTML autocontido a
# partir da fonte nfse-stack.src.html e tira o PNG claro e escuro com o
# Chromium headless do Playwright.
#
# O Playwright vem do Nix (python3 + playwright e os navegadores do
# nixpkgs), nunca de pip/brew: as versões do pacote Python e dos navegadores
# saem do mesmo nixpkgs e por isso casam — misturar as duas origens dá
# "Executable doesn't exist" na hora de abrir o Chromium.
#
# Depende da skill stack-diagram (build.py, render_png.py e as fontes Geist e
# JetBrains Mono embutidas no HTML).
set -euo pipefail
cd "$(dirname "$0")/../docs/diagram"

SKILL="${STACK_DIAGRAM_SKILL:-$HOME/.claude/skills/stack-diagram}"
[ -f "$SKILL/scripts/build.py" ] || { echo "🛑 skill stack-diagram não encontrada em $SKILL"; exit 1; }

echo "[1/3] HTML autocontido (ícones e fontes embutidos como data URI)"
python3 "$SKILL/scripts/build.py" nfse-stack.src.html icons nfse-stack.html --fonts "$SKILL/assets/fonts"

echo "[2/3] Python com Playwright e navegadores pelo Nix (cache local após a 1ª vez)"
py_env=$(nix build --no-link --print-out-paths --impure --expr \
  'let p = import (builtins.getFlake "nixpkgs") {}; in p.python3.withPackages (ps: [ ps.playwright ])')
browsers=$(nix build --no-link --print-out-paths nixpkgs#playwright-driver.browsers)

echo "[3/3] PNG claro e escuro, escala 2x, só o quadro do diagrama"
# PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS: a checagem de dependências do
# host é pensada para Linux com apt e falha à toa no macOS com os binários do Nix.
PLAYWRIGHT_BROWSERS_PATH="$browsers" PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS=true \
  "$py_env/bin/python3" "$SKILL/scripts/render_png.py" nfse-stack.html nfse-stack.png --selector .canvas --scale 2

ls -la nfse-stack.png nfse-stack-dark.png
