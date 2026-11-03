-- Certificado A1 cifrado por conta (deploy no Fly, onde não há pass). O
-- conteúdo é .pfx + senha selados com AES-256-GCM pela chave mestra do
-- servidor (secret NFSE_CERT_KEY); o banco sozinho não abre nada.
CREATE TABLE sealed_certificates (
  account_id INTEGER PRIMARY KEY,
  sealed BLOB NOT NULL,
  updated_at TEXT NOT NULL
);
