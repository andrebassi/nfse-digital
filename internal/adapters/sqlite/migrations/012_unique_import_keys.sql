-- Importar duas vezes (portal automático depois do certificado, um clique
-- repetido, duas abas) nunca duplica dado: a mesma NFS-e (chave de acesso) e
-- o mesmo tomador (CPF/CNPJ) existem uma vez só por conta. O código já
-- confere antes de gravar; o índice é a garantia contra duas gravações
-- correndo juntas. Medido em 30/10/2026: nenhuma duplicata nos dados reais.
CREATE UNIQUE INDEX invoices_access_key ON invoices (account_id, access_key) WHERE access_key <> '';
CREATE UNIQUE INDEX customers_document ON customers (account_id, document) WHERE document <> '';
