-- Percentual aproximado dos tributos pela alíquota do Simples (pTotTribSN),
-- em centésimos de ponto (6,00% = 600). A SEFIN exige para ME/EPP desde que
-- passou a recusar indTotTrib com E0712 (produção, 02/11/2026). Zero = não
-- informado: a emissão de ME/EPP pede o valor antes de montar a DPS.
ALTER TABLE issuers ADD COLUMN simples_tax_rate INTEGER NOT NULL DEFAULT 0;
