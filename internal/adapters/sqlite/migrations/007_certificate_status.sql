-- Dados PÚBLICOS do certificado A1 em uso (titular, CNPJ, AC, validade), para a
-- tela de Configurações mostrar sem abrir o pass a cada visita. O .pfx e a
-- senha continuam só no pass: nada aqui assina coisa alguma.
-- portal_authorized_at: quando o dono autorizou usar o certificado para LER a
-- base nacional (importar do portal, sincronizar); vazio = não autorizado.
CREATE TABLE certificate_status (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  holder_name TEXT NOT NULL DEFAULT '',
  cnpj TEXT NOT NULL DEFAULT '',
  issuer_name TEXT NOT NULL DEFAULT '',
  not_before TEXT NOT NULL DEFAULT '',
  not_after TEXT NOT NULL DEFAULT '',
  checked_at TEXT NOT NULL DEFAULT '',
  portal_authorized_at TEXT NOT NULL DEFAULT ''
);
