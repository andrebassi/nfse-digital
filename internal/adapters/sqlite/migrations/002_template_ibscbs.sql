-- Grupo IBS/CBS (reforma tributária) no modelo de serviço. Vazio usa os
-- padrões da NFS-e real de 05/10/2026: operação 050101, CST 000, classe 000001.
ALTER TABLE templates ADD COLUMN ibscbs_operation TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN ibscbs_cst TEXT NOT NULL DEFAULT '';
ALTER TABLE templates ADD COLUMN ibscbs_class TEXT NOT NULL DEFAULT '';
