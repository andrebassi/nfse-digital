import { createGetUrl } from 'fumadocs-core/source';

// Nome que aparece no topo e nos títulos.
export const appName = 'nfse-digital';
// Painel publicado: destino dos links "Abrir o painel" e da API.
export const panelUrl = 'https://nfse.bassi.engineer';
export const docsRoute = '/docs';
export const docsImageRoute = '/og/docs';
export const docsContentRoute = '/llms.mdx/docs';

// Repositório público: alimenta o link do GitHub e o "editar no GitHub".
// contentDir é onde os .mdx moram dentro do repo (o site fica em site/).
export const gitConfig = {
  user: 'andrebassi',
  repo: 'nfse-digital',
  branch: 'main',
  contentDir: 'site/content/docs',
};

const getContentUrl = createGetUrl(docsContentRoute);

export function getPageMarkdownUrl(page: { slugs: string[]; locale?: string }) {
  const segments = [...page.slugs, 'content.md'];

  return { segments, url: getContentUrl(segments, page.locale) };
}

const getImageUrl = createGetUrl(docsImageRoute);

export function getPageImageUrl(page: { slugs: string[]; locale?: string }) {
  const segments = [...page.slugs, 'image.png'];

  return { segments, url: getImageUrl(segments, page.locale) };
}
