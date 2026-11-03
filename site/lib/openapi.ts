import { createOpenAPI } from 'fumadocs-openapi/server';

// Contrato da API lido no build (só no servidor): o mesmo openapi.json do
// painel, copiado para public/ pelo scripts/docs-site.sh com o servers da
// produção. Gera as páginas de operação e alimenta o playground.
export const openapi = createOpenAPI({
  input: ['./public/openapi.json'],
});
