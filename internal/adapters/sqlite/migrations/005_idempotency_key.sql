-- Chave de idempotência da emissão (story 4.2): o agente manda a mesma chave
-- ao repetir um pedido, e o índice único garante que a segunda chamada não
-- vire uma segunda nota. Vazio = emissão sem chave (CLI e painel).
ALTER TABLE invoices ADD COLUMN idempotency_key TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX invoices_idempotency_key ON invoices (idempotency_key) WHERE idempotency_key <> '';
