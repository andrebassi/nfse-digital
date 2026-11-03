#!/bin/bash
# Publica o painel no Fly (app nfse-digital, região gru, 1 máquina que dorme,
# sem volume — o banco é o Turso). Idempotente: rodar de novo só republica.
#
# Segredos saem do pass e vão direto para `fly secrets import` pelo stdin,
# sem passar por argumento nem arquivo:
#   bassi/flyio/api-token        token do Fly
#   bassi/turso/nfse-auth-token  TURSO_AUTH_TOKEN
#   bassi/nfse/cert-vault-key    NFSE_CERT_KEY — gerada aqui na 1ª vez. 🛑 Perder
#                                esta chave = os certificados selados no Turso
#                                não abrem mais (cada conta reenvia o .pfx).
# NFSE_SIGNUP_EMAILS (quem pode criar conta) vem do ambiente, padrão o dono.
set -euo pipefail
cd "$(dirname "$0")/.."

# App e domínio: os do dono por padrão; outra instância troca pelas variáveis
# (e ajusta o fly.toml, que traz os mesmos dois valores).
APP="${NFSE_FLY_APP:-nfse-digital}"
HOST="${NFSE_PUBLIC_HOST:-nfse.bassi.engineer}"
SIGNUP_EMAILS="${NFSE_SIGNUP_EMAILS:-andrebassi@gmail.com}"
# O pass precisa achar o gpg2 roteador antes do gpg do Nix (skill pass-route).
export PATH="$HOME/.local/bin:/usr/bin:/bin:$PATH:/etc/profiles/per-user/$USER/bin"

FLY_API_TOKEN="$(timeout 120s pass show bassi/flyio/api-token)"
export FLY_API_TOKEN

echo "[1/6] app $APP"
# Pergunta pelo próprio app, não por grep no JSON da lista: o formato do JSON
# mudou e o grep não casava, e o script tentava criar de novo (30/10/2026).
if ! timeout 30s fly apps show "$APP" >/dev/null 2>&1 && ! timeout 30s fly status -a "$APP" >/dev/null 2>&1; then
  timeout 60s fly apps create "$APP" --org personal
fi

echo "[2/6] chave do cofre do certificado"
# Existência pelo ARQUIVO, nunca por `pass show` falhar: decifrar falha também
# quando a aprovação do celular não vem, e aí uma chave nova por cima da boa
# trancaria todos os certificados já selados no Turso.
if [ ! -f "${PASSWORD_STORE_DIR:-$HOME/.password-store}/bassi/nfse/cert-vault-key.gpg" ]; then
  # 32 bytes aleatórios em base64 = AES-256 (vaultcert.ParseKey).
  timeout 10s openssl rand -base64 32 | timeout 120s pass insert -m -f bassi/nfse/cert-vault-key >/dev/null
  echo "  gerada e guardada em pass bassi/nfse/cert-vault-key"
fi

echo "[3/6] secrets (stage: valem no deploy seguinte)"
{
  printf 'TURSO_AUTH_TOKEN=%s\n' "$(timeout 120s pass show bassi/turso/nfse-auth-token)"
  printf 'NFSE_CERT_KEY=%s\n' "$(timeout 120s pass show bassi/nfse/cert-vault-key)"
  printf 'NFSE_SIGNUP_EMAILS=%s\n' "$SIGNUP_EMAILS"
} | timeout 60s fly secrets import --stage -a "$APP" >/dev/null
timeout 30s fly secrets list -a "$APP"

echo "[4/6] CSS do painel (embutido no binário)"
task css

echo "[5/6] deploy (builder remoto do Fly, 1 máquina, sem HA)"
# --depot=false: o builder depot estourou o prazo em 30/10/2026
# (deadline_exceeded, em laço); o builder próprio do Fly sobe sob demanda.
fly deploy -a "$APP" --remote-only --depot=false --ha=false --wait-timeout 300
timeout 60s fly scale count 1 -a "$APP" --yes

echo "[6/6] certificado TLS de $HOST"
timeout 60s fly certs add "$HOST" -a "$APP" 2>&1 | grep -v "already" || true
timeout 60s fly certs show "$HOST" -a "$APP" | head -20
timeout 30s fly ips list -a "$APP"
