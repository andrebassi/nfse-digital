#!/bin/bash
# Gate de cobertura do nfse: mede linha por pacote e REPROVA (rc=1) quando
# algum pacote fica abaixo do piso. `go test -cover` sozinho só imprime e sai 0.
#
# Pisos: internal/domain 100%; demais pacotes de internal/ 90%.
# Isentos: cmd/ (wiring) e internal/ports (só interfaces, sem instrução).
# COVERAGE_FLOOR sobrescreve o piso de todos — usado na prova de falha
# (COVERAGE_FLOOR=101 tem que reprovar).
#
# Limite declarado: Go não mede branch nativamente. Os branches são cobertos
# por testes de tabela que exercitam os dois lados de cada decisão; o número
# medido aqui é de linha (statements).
set -euo pipefail

profile_dir="$(mktemp -d)"
trap 'rm -rf "$profile_dir"' EXIT

status=0
packages=$(go list ./internal/... | grep -v '/internal/ports$')
for package in $packages; do
  profile="$profile_dir/$(echo "$package" | tr '/' '_').out"
  go test -count=1 -coverprofile="$profile" "$package" >/dev/null

  # Pacote sem nenhuma instrução gera perfil vazio: conta como 0%, não como aprovado.
  total=$(go tool cover -func="$profile" | awk '/^total:/ {gsub("%","",$3); print $3}')
  total=${total:-0}

  floor=90
  [[ "$package" == */internal/domain ]] && floor=100
  floor=${COVERAGE_FLOOR:-$floor}

  if awk -v t="$total" -v f="$floor" 'BEGIN { exit (t+0 >= f+0) ? 0 : 1 }'; then
    printf '✅ %-45s %6s%% (piso %s%%)\n' "$package" "$total" "$floor"
  else
    printf '🛑 %-45s %6s%% (piso %s%%)\n' "$package" "$total" "$floor"
    status=1
  fi
done

if [ "$status" -ne 0 ]; then
  echo "🛑 cobertura abaixo do piso"
fi
exit "$status"
