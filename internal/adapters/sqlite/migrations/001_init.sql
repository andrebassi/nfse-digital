-- Esquema inicial do nfse-digital. Valores em centavos (INTEGER), datas em
-- texto RFC 3339, alíquotas e códigos como texto cru.

CREATE TABLE account (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  name TEXT NOT NULL,
  email TEXT NOT NULL,
  password_hash BLOB NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE issuer (
  id INTEGER PRIMARY KEY CHECK (id = 1),
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

CREATE TABLE customers (
  id INTEGER PRIMARY KEY,
  foreign_customer INTEGER NOT NULL DEFAULT 0,
  document TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  city_code TEXT NOT NULL DEFAULT '',
  city_name TEXT NOT NULL DEFAULT '',
  zip TEXT NOT NULL DEFAULT '',
  district TEXT NOT NULL DEFAULT '',
  street TEXT NOT NULL DEFAULT '',
  number TEXT NOT NULL DEFAULT '',
  complement TEXT NOT NULL DEFAULT '',
  country TEXT NOT NULL DEFAULT ''
);

CREATE TABLE templates (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  national_tax_code TEXT NOT NULL,
  municipal_tax_code TEXT NOT NULL DEFAULT '',
  nbs TEXT NOT NULL DEFAULT '',
  service_city_code TEXT NOT NULL,
  service_city_name TEXT NOT NULL DEFAULT '',
  default_amount_cents INTEGER NOT NULL DEFAULT 0,
  simples_tax_rate TEXT NOT NULL DEFAULT '',
  issqn_taxation TEXT NOT NULL DEFAULT '',
  issqn_retention TEXT NOT NULL DEFAULT '',
  export INTEGER NOT NULL DEFAULT 0,
  customer_id INTEGER NOT NULL DEFAULT 0
);

-- A unicidade de (ambiente, série, número) é a trava contra emitir duas
-- notas com o mesmo número de DPS.
CREATE TABLE invoices (
  id INTEGER PRIMARY KEY,
  environment TEXT NOT NULL,
  series TEXT NOT NULL,
  dps_number INTEGER NOT NULL,
  customer_id INTEGER NOT NULL,
  template_id INTEGER NOT NULL DEFAULT 0,
  amount_cents INTEGER NOT NULL,
  competence TEXT NOT NULL,
  issued_at TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  service_code TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('pending', 'authorized', 'rejected', 'cancelled')),
  access_key TEXT NOT NULL DEFAULT '',
  nfse_number TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  simulated INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX invoices_dps ON invoices (environment, series, dps_number) WHERE dps_number > 0;

CREATE TABLE schedules (
  id INTEGER PRIMARY KEY,
  template_id INTEGER NOT NULL,
  customer_id INTEGER NOT NULL,
  amount_cents INTEGER NOT NULL,
  day INTEGER NOT NULL CHECK (day BETWEEN 1 AND 31),
  competence TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  notify_eve INTEGER NOT NULL DEFAULT 1,
  send_to_customer INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1
);
