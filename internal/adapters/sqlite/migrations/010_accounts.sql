-- Várias contas, cada uma com a própria empresa. Tudo o que existia até aqui
-- pertence à conta 1 (o dono original). Tabelas que só aceitavam id = 1
-- (account, issuer, certificate_status) são refeitas, porque o SQLite não
-- remove CHECK com ALTER TABLE.

CREATE TABLE accounts (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT NOT NULL UNIQUE,
  password_hash BLOB NOT NULL,
  created_at TEXT NOT NULL
);
INSERT INTO accounts (id, name, email, password_hash, created_at) SELECT id, name, email, password_hash, created_at FROM account;
DROP TABLE account;

-- Emitente: um por conta, chaveado pela própria conta.
CREATE TABLE issuers (
  account_id INTEGER PRIMARY KEY,
  legal_name TEXT NOT NULL,
  cnpj TEXT NOT NULL,
  municipal_registration TEXT NOT NULL DEFAULT '',
  city_code TEXT NOT NULL,
  city_name TEXT NOT NULL DEFAULT '',
  simples_option TEXT NOT NULL DEFAULT '',
  simples_regime TEXT NOT NULL DEFAULT '',
  special_regime TEXT NOT NULL DEFAULT '',
  environment TEXT NOT NULL CHECK (environment IN ('homologation', 'production')),
  series TEXT NOT NULL,
  next_dps_number INTEGER NOT NULL CHECK (next_dps_number > 0)
);
INSERT INTO issuers SELECT 1, legal_name, cnpj, municipal_registration, city_code, city_name, simples_option, simples_regime,
  special_regime, environment, series, next_dps_number FROM issuer;
DROP TABLE issuer;

-- Estado público do certificado: um por conta.
CREATE TABLE certificate_statuses (
  account_id INTEGER PRIMARY KEY,
  holder_name TEXT NOT NULL DEFAULT '',
  cnpj TEXT NOT NULL DEFAULT '',
  issuer_name TEXT NOT NULL DEFAULT '',
  not_before TEXT NOT NULL DEFAULT '',
  not_after TEXT NOT NULL DEFAULT '',
  checked_at TEXT NOT NULL DEFAULT '',
  portal_authorized_at TEXT NOT NULL DEFAULT ''
);
INSERT INTO certificate_statuses SELECT 1, holder_name, cnpj, issuer_name, not_before, not_after, checked_at, portal_authorized_at
  FROM certificate_status;
DROP TABLE certificate_status;

-- Último contato do agente: por conta e cliente.
CREATE TABLE agent_contacts_by_account (
  account_id INTEGER NOT NULL,
  client TEXT NOT NULL,
  tool TEXT NOT NULL,
  contacted_at TEXT NOT NULL,
  PRIMARY KEY (account_id, client)
);
INSERT INTO agent_contacts_by_account SELECT 1, client, tool, contacted_at FROM agent_contacts;
DROP TABLE agent_contacts;

-- Demais tabelas: a coluna da conta, com o que já existia na conta 1.
ALTER TABLE customers ADD COLUMN account_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE templates ADD COLUMN account_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE invoices ADD COLUMN account_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE schedules ADD COLUMN account_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE api_tokens ADD COLUMN account_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE passkeys ADD COLUMN account_id INTEGER NOT NULL DEFAULT 1;

-- Numeração da DPS e chave de idempotência valem por conta: duas empresas
-- podem ter a mesma série e o mesmo número.
DROP INDEX invoices_dps;
CREATE UNIQUE INDEX invoices_dps ON invoices (account_id, environment, series, dps_number) WHERE dps_number > 0;
DROP INDEX invoices_idempotency_key;
CREATE UNIQUE INDEX invoices_idempotency_key ON invoices (account_id, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX customers_account ON customers (account_id);
CREATE INDEX templates_account ON templates (account_id);
CREATE INDEX invoices_account ON invoices (account_id);
CREATE INDEX schedules_account ON schedules (account_id);
