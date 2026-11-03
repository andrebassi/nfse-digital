-- Envio da nota por e-mail (Gmail com senha de app), como no SaaS usado antes.
--
-- email_senders: o Gmail que envia, um por conta. sealed_password é a senha
-- de app selada com AES-256-GCM pela chave do servidor (NFSE_CERT_KEY no Fly,
-- arquivo de chave local fora dele): o banco sozinho não abre a senha.
CREATE TABLE email_senders (
  account_id INTEGER PRIMARY KEY,
  address TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  sealed_password BLOB NOT NULL,
  verified_at TEXT NOT NULL DEFAULT ''
);

-- customer_emails: destinatários, texto e anexos de cada tomador. Sem linha,
-- vale o padrão (Para = e-mail do cadastro, sem envio automático).
CREATE TABLE customer_emails (
  account_id INTEGER NOT NULL,
  customer_id INTEGER NOT NULL,
  auto_send INTEGER NOT NULL DEFAULT 0,
  to_addresses TEXT NOT NULL,
  cc_addresses TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL,
  body TEXT NOT NULL,
  attach_pdf INTEGER NOT NULL DEFAULT 1,
  attach_xml INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (account_id, customer_id)
);

-- invoice_emails: cada envio (ou falha) de uma nota, para o detalhe da nota
-- mostrar se o tomador recebeu e quando.
CREATE TABLE invoice_emails (
  id INTEGER PRIMARY KEY,
  account_id INTEGER NOT NULL,
  invoice_id INTEGER NOT NULL,
  sent_at TEXT NOT NULL,
  to_addresses TEXT NOT NULL,
  automatic INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX invoice_emails_by_invoice ON invoice_emails (account_id, invoice_id);
