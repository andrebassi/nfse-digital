#!/bin/bash
# Importa o certificado A1 (.pfx) para o pass: o arquivo vira base64 em
# bassi/nfse/cert-a1-pfx e a senha vai para bassi/nfse/cert-a1-password.
# Nada é escrito em disco em claro, e a senha nunca aparece na saída.
#
# Uso: scripts/import-cert.sh "<caminho do .pfx>"
#
# A certificadora entrega o arquivo com a senha no próprio nome
# ("... SENHA 1234.pfx"); quando o nome segue esse padrão, a senha sai dali.
# Sem o padrão, a senha é lida da variável CERT_PASSWORD.
set -euo pipefail

pfx_path="${1:?informe o caminho do .pfx}"
[ -s "$pfx_path" ] || { echo "🛑 arquivo não encontrado ou vazio: $pfx_path"; exit 1; }

# Regex: captura o que vem depois de "SENHA " até a extensão .pfx.
file_name="$(basename "$pfx_path")"
if [[ "$file_name" =~ SENHA[[:space:]]+(.+)\.pfx$ ]]; then
  CERT_PASSWORD="${BASH_REMATCH[1]}"
fi
: "${CERT_PASSWORD:?senha não encontrada no nome do arquivo nem em CERT_PASSWORD}"
export CERT_PASSWORD

# Confere se a senha abre o arquivo antes de gravar qualquer coisa no pass.
# -passin env: evita a senha na linha de comando (visível no ps).
cert_pem="$(openssl pkcs12 -in "$pfx_path" -passin env:CERT_PASSWORD -nokeys -clcerts 2>/dev/null)" \
  || { echo "🛑 a senha não abre o .pfx"; exit 1; }
echo "certificado:"
echo "$cert_pem" | openssl x509 -noout -subject -issuer -enddate -nameopt utf8,sep_comma_plus | sed 's/^/  /'

# Gravação: -m lê várias linhas do stdin, -f sobrescreve sem perguntar.
base64 < "$pfx_path" | pass insert -m -f bassi/nfse/cert-a1-pfx >/dev/null
printf '%s\n' "$CERT_PASSWORD" | pass insert -m -f bassi/nfse/cert-a1-password >/dev/null
unset CERT_PASSWORD

echo "gravado no pass:"
pass ls bassi/nfse | sed 's/^/  /'
