// Gera uma página .mdx por operação da API em content/docs/api/endpoints,
// a partir do contrato (public/openapi.json). Roda no build, antes do next
// build, pelo scripts/docs-site.sh: as páginas nunca divergem do contrato.
// Arquivo de build do site Fumadocs (TypeScript por escolha do dono).
import { readFile, writeFile } from 'node:fs/promises';
import { generateFiles } from 'fumadocs-openapi';
import { openapi } from '../lib/openapi.ts';

// output é a pasta das páginas geradas; o meta.json dela ganha o título do menu.
const output = './content/docs/api/endpoints';

await generateFiles({
  input: openapi,
  output,
  // Uma página por operação, nomeada pelo operationId (emitInvoice.mdx…).
  per: 'operation',
  // A descrição de cada operação vem do contrato; ela é texto simples, sem
  // chaves soltas que quebrariam o MDX.
  includeDescription: true,
  meta: true,
});

// O gerador escreve só a ordem das páginas; sem título, o menu mostraria o
// nome da pasta ("endpoints").
const metaPath = `${output}/meta.json`;
const meta = JSON.parse(await readFile(metaPath, 'utf8'));
await writeFile(metaPath, JSON.stringify({ title: 'Operações (playground)', ...meta }, null, 2) + '\n');
