-- Passkeys do dono (story 4.4). credential guarda a credencial WebAuthn em
-- JSON: chave PÚBLICA, contador de uso e flags. Não há segredo aqui — a chave
-- privada nunca sai do aparelho (Touch ID, Windows Hello, chave de segurança).
CREATE TABLE passkeys (
  id INTEGER PRIMARY KEY,
  credential_id BLOB NOT NULL UNIQUE,
  credential BLOB NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT ''
);
