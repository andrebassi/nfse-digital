-- Último contato de cada agente pelo MCP (tela de Integrações → Testar
-- conexão). O servidor MCP roda em outro processo, por stdio; o banco é o
-- único lugar que ele e o painel compartilham.
CREATE TABLE agent_contacts (
  client TEXT PRIMARY KEY,
  tool TEXT NOT NULL,
  contacted_at TEXT NOT NULL
);
