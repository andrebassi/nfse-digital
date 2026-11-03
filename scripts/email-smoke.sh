#!/bin/bash
# Smoke test do envio por e-mail contra o binário de verdade, SEM Gmail: sobe
# o Mailpit (servidor SMTP de teste, via Nix) e aponta o painel para ele com
# NFSE_SMTP_ADDR. Banco temporário, conta descartável, senha de app fictícia
# (o Mailpit aceita qualquer login). Confere, pela API do Mailpit:
#   1. salvar o envio testa o login e guarda;
#   2. "Enviar teste" chega com [TESTE], sem cópia, com os dois anexos;
#   3. o botão "Enviar por e-mail" de uma nota autorizada (a NFS-e de
#      referência da pasta testdata/) chega com o DANFSe e o XML de verdade.
#
# Uso: scripts/email-smoke.sh            (precisa de bin/nfse; `task build` antes)
#      KEEP=1 scripts/email-smoke.sh     (deixa painel e Mailpit no ar para ver
#                                         no navegador; caixa em http://127.0.0.1:8026)
# Sai rc=0 só se todas as checagens passarem; cada falha soma 1 no rc.
set -uo pipefail

port=8797
base="http://127.0.0.1:$port"
smtp_port=1026
mailpit_ui=8026
workdir="$(mktemp -d)"
db="$workdir/smoke.db"
jar="$workdir/cookies.txt"
failures=0
if [ "${KEEP:-0}" != 1 ]; then
  trap 'kill "$server_pid" "$mailpit_pid" 2>/dev/null; rm -rf "$workdir"' EXIT
fi

# check imprime o resultado de uma verificação e conta as falhas.
check() {
  if [ "$2" = "ok" ]; then echo "✅ $1"; else echo "🛑 $1"; failures=$((failures + 1)); fi
}

# csrf lê o token do formulário de uma página.
csrf() {
  curl --max-time 10 -s -b "$jar" -c "$jar" "$base$1" | grep -o 'name="csrf" value="[^"]*"' | head -1 | cut -d'"' -f4
}

# wait_http espera a URL responder (até 10 s).
wait_http() {
  for _ in $(seq 1 100); do
    curl --max-time 2 -s -o /dev/null "$1" && return 0
    sleep 0.1
  done
  echo "🛑 $1 não subiu"; exit 1
}

# mailpit_json lê a API do Mailpit.
mailpit_json() { curl --max-time 10 -s "http://127.0.0.1:$mailpit_ui/api/v1/$1"; }

# Mailpit aceita qualquer usuário e senha (--smtp-auth-accept-any) e deixa o
# AUTH PLAIN sem TLS (--smtp-auth-allow-insecure): o cliente Go só manda a
# senha sem TLS para 127.0.0.1, então nada sai em claro pela rede.
nix shell nixpkgs#mailpit --command mailpit --smtp "127.0.0.1:$smtp_port" --listen "127.0.0.1:$mailpit_ui" \
  --smtp-auth-accept-any --smtp-auth-allow-insecure --database "$workdir/mailpit.db" >>"$workdir/mailpit.log" 2>&1 &
mailpit_pid=$!
wait_http "http://127.0.0.1:$mailpit_ui/api/v1/info"

# NFSE_CERT_KEY vazia + HOME temporário: a chave do cofre nasce no workdir,
# nunca no ~/.local/share do dono.
HOME="$workdir" NFSE_CERT_KEY="" NFSE_SMTP_ADDR="127.0.0.1:$smtp_port" \
  ./bin/nfse serve --port "$port" --db "$db" >>"$workdir/serve.log" 2>&1 &
server_pid=$!
wait_http "$base/login"

password="smoke-$(openssl rand -hex 8)"
token=$(csrf /signup)
curl --max-time 10 -s -o /dev/null -b "$jar" -c "$jar" -X POST "$base/signup" \
  --data-urlencode "csrf=$token" --data-urlencode "name=Dona Exemplo" \
  --data-urlencode "email=dona@exemplo.example" --data-urlencode "password=$password"
# Certificado fictício só para abrir a trava do painel; nenhum A1 é lido.
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
  --data-urlencode "name=AURORA SISTEMAS LTDA" --data-urlencode "document=12.345.678/0001-95" \
  --data-urlencode "email=global@cliente.example"

echo "== aba Envio por e-mail"
page=$(curl --max-time 10 -s -b "$jar" "$base/customers/1/email")
check "aba abre com o e-mail do cadastro em Para" "$(grep -q 'value="global@cliente.example"' <<<"$page" && echo ok)"
check "prévia com a nota de exemplo 123" "$(grep -q 'NFS-e nº 123 - EXEMPLO TECNOLOGIA LTDA' <<<"$page" && echo ok)"

# form_post envia o formulário da aba com a ação dada e devolve o status HTTP.
form_post() {
  local token
  token=$(csrf /customers/1/email)
  curl --max-time 30 -s -o "$workdir/last.html" -w '%{http_code}' -b "$jar" -c "$jar" -X POST "$base/customers/1/email" \
    --data-urlencode "csrf=$token" --data-urlencode "action=$1" \
    --data-urlencode "sender_address=dono@gmail.example" --data-urlencode "sender_name=Financeiro · Exemplo" \
    --data-urlencode "sender_password=$2" --data-urlencode "to=global@cliente.example" \
    --data-urlencode "cc=copia@cliente.example" --data-urlencode "subject=NFS-e nº {numero} - {emitente}" \
    --data-urlencode "body=Olá,

Segue a nota nº {numero} de {mesCompetencia}, no valor de {valor}.
{linkConsulta}" \
    --data-urlencode "attach_pdf=on" --data-urlencode "attach_xml=on" --data-urlencode "test_to=dona@exemplo.example"
}

check "salvar sem senha na primeira vez é recusado (422)" "$([ "$(form_post save '')" = 422 ] && echo ok)"
check "salvar com senha de app testa o login e grava (303)" "$([ "$(form_post save 'abcd efgh ijkl mnop')" = 303 ] && echo ok)"
sealed=$(sqlite3 "$db" "SELECT hex(sealed_password) FROM email_senders WHERE account_id = 1")
plain_hex=$(printf 'abcdefghijklmnop' | xxd -p | tr 'a-f' 'A-F')
check "senha guardada cifrada (não aparece em claro no banco)" "$([ -n "$sealed" ] && ! grep -q "$plain_hex" <<<"$sealed" && echo ok)"
check "chave do cofre nasceu no HOME temporário com 600" "$([ "$(stat -f '%Lp' "$workdir/.local/share/nfse-digital/secret.key" 2>/dev/null)" = 600 ] && echo ok)"

echo "== e-mail de teste"
check "Enviar teste responde 200 (senha guardada)" "$([ "$(form_post test '')" = 200 ] && grep -q 'E-mail de teste enviado' "$workdir/last.html" && echo ok)"
messages=$(mailpit_json messages)
check "chegou 1 e-mail no Mailpit" "$([ "$(jq -r '.total' <<<"$messages")" = 1 ] && echo ok)"
subject=$(jq -r '.messages[0].Subject' <<<"$messages")
check "assunto com [TESTE] e marcador resolvido ($subject)" "$([ "$subject" = "[TESTE] NFS-e nº 123 - EXEMPLO TECNOLOGIA LTDA" ] && echo ok)"
check "teste sem cópia" "$([ "$(jq -r '.messages[0].Cc | length' <<<"$messages")" = 0 ] && echo ok)"
first=$(jq -r '.messages[0].ID' <<<"$messages")
detail=$(mailpit_json "message/$first")
check "dois anexos: danfse-123.pdf e nfse-123.xml" "$([ "$(jq -r '[.Attachments[].FileName] | join(",")' <<<"$detail")" = "danfse-123.pdf,nfse-123.xml" ] && echo ok)"
check "remetente com nome (Financeiro · Exemplo)" "$(jq -r '.From.Name' <<<"$detail" | grep -q 'Financeiro · Exemplo' && echo ok)"
xml_part=$(jq -r '.Attachments[] | select(.FileName == "nfse-123.xml") | .PartID' <<<"$detail")
check "XML de exemplo marcado como SIMULAÇÃO" "$(curl --max-time 10 -s "http://127.0.0.1:$mailpit_ui/api/v1/message/$first/part/$xml_part" | grep -q 'SIMULAÇÃO' && echo ok)"

echo "== nota autorizada: botão Enviar por e-mail"
# Nota autorizada com o XML de referência (NFS-e real anonimizada).
sqlite3 "$db" "INSERT INTO invoices (account_id, environment, series, dps_number, customer_id, amount_cents, competence, issued_at, description, status, access_key, nfse_number, nfse_xml)
  VALUES (1, 'homologation', '900', 77, 1, 150000, '2026-11-01T00:00:00-03:00', '2026-11-02T19:00:00-03:00', 'Suporte técnico - novembro', 'authorized',
  '35057082200000000000000000000000000000000000000077', '77', readfile('testdata/nfse-reference.xml'))"
invoice_id=$(sqlite3 "$db" "SELECT id FROM invoices WHERE dps_number = 77")
token=$(csrf "/invoices?period=90&id=$invoice_id")
location=$(curl --max-time 30 -s -o /dev/null -w '%{redirect_url}' -b "$jar" -c "$jar" -X POST "$base/invoices/$invoice_id/email" --data-urlencode "csrf=$token")
check "botão redireciona com E-mail enviado" "$(grep -q 'E-mail+enviado\|E-mail%20enviado' <<<"$location" && echo ok)"
messages=$(mailpit_json messages)
check "chegou o 2º e-mail" "$([ "$(jq -r '.total' <<<"$messages")" = 2 ] && echo ok)"
check "com cópia para copia@cliente.example" "$([ "$(jq -r '.messages[0].Cc[0].Address' <<<"$messages")" = copia@cliente.example ] && echo ok)"
second=$(jq -r '.messages[0].ID' <<<"$messages")
detail=$(mailpit_json "message/$second")
check "anexos da nota real (danfse-77.pdf, nfse-77.xml)" "$([ "$(jq -r '[.Attachments[].FileName] | join(",")' <<<"$detail")" = "danfse-77.pdf,nfse-77.xml" ] && echo ok)"
check "corpo com R\$ 1.500,00 e o link de consulta" "$(jq -r '.Text' <<<"$detail" | grep -q 'R\$ 1.500,00' && jq -r '.Text' <<<"$detail" | grep -q 'ConsultaPublica' && echo ok)"
page=$(curl --max-time 10 -s -b "$jar" "$base/invoices?period=90&id=$invoice_id")
check "histórico no detalhe da nota" "$(grep -q 'manual · global@cliente.example' <<<"$page" && echo ok)"

if [ "${KEEP:-0}" = 1 ]; then
  awk '$6 == "session" {print $7}' "$jar" | tail -1 >"$workdir/session"
  echo "no ar: painel $base (pid $server_pid) · Mailpit http://127.0.0.1:$mailpit_ui (pid $mailpit_pid) · sessão em $workdir/session · nota $invoice_id"
fi
echo "falhas: $failures"
exit "$failures"
