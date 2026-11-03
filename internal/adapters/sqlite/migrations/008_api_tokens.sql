-- Tokens de API do MCP por HTTP (tela de Integrações). Só o SHA-256 do token
-- é guardado: o valor aparece uma única vez, na hora de gerar, e quem ler o
-- banco não consegue usá-lo. hint são os 4 últimos caracteres, para o dono
-- reconhecer qual token é qual na lista.
CREATE TABLE api_tokens (
  id INTEGER PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  hint TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT ''
);
