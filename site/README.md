# Site de documentação do nfse-digital

Fonte do **[nfse-docs.bassi.engineer](https://nfse-docs.bassi.engineer)**: manual de uso, API REST com **playground** por operação, SDKs, agentes de IA, self-hosting e a referência da API. Mesmo desenho da documentação da Hodle (docs.hodle.com.br), que foi a referência: Fumadocs com layout *notebook* e o Scalar embutido na aba de referência.

## Stack

Versões instaladas, lidas do `package-lock.json` (02/11/2026):

| Peça | Pacote | Versão | Papel |
|---|---|---|---|
| Framework | [Next.js](https://nextjs.org/) (App Router) | 16.4.2 | roteamento e **export estático** (`output: 'export'`) — não há servidor em produção |
| UI | [React](https://react.dev/) | 19.3.1 | |
| Documentação | [Fumadocs](https://fumadocs.dev/) — `fumadocs-core`, `fumadocs-ui` | 16.17.10 | layout *notebook* (abas no topo + menu lateral), busca, TOC, páginas, `llms.txt` |
| Conteúdo | `fumadocs-mdx` | 15.4.10 | lê os `.mdx` de `content/docs` com frontmatter validado |
| Playground | [`fumadocs-openapi`](https://fumadocs.dev/docs/integrations/openapi) | 12.5.8 | uma página por operação do contrato, com o playground (token, parâmetros, corpo, Enviar) |
| Referência | [`@scalar/api-reference-react`](https://github.com/scalar/scalar) | 0.9.87 | `/api-reference`: referência navegável, tema `kepler`, layout `modern` |
| Busca | `zbsearch` (dentro do fumadocs-core) | 4.0.1 | índice gerado no build (`/api/search`, ~644 KB) e consultado **no navegador**; stemming em português |
| Destaque de código | [Shiki](https://shiki.style/) | 4.6.0 | blocos de código e abas cURL / Go / Python / TypeScript |
| Estilo | [Tailwind CSS](https://tailwindcss.com/) | 4.3.4 | + presets de CSS do Fumadocs e do fumadocs-openapi |
| Tipos | TypeScript | 7.0.2 | |
| Hospedagem | [Cloudflare Pages](https://pages.cloudflare.com/) | — | projeto `nfse-docs`, CDN, sem custo; DNS na zona `bassi.engineer` do próprio Cloudflare |
| Runtime de build | Node.js 24 via Nix (`nixpkgs#nodejs_24`) | — | nada de Node do sistema |

O site é TypeScript/Next.js por decisão explícita do dono, contra a regra que barra código novo em JS/TS: é o que o Fumadocs exige. O resto do projeto continua em Go.

## Como foi montado

1. **Esqueleto** — `npx create-fumadocs-app@16.2.28 site --template +next+fuma-docs-mdx+static --search orama`. O template `+static` já sai com `output: 'export'` e a rota de busca no modo estático (`staticGET`).
2. **Layout notebook** — `app/docs/layout.tsx` usa `fumadocs-ui/layouts/notebook` com `tabMode="navbar"`: cada pasta de `content/docs` com `"root": true` no `meta.json` vira uma aba no topo.
3. **pt-BR** — `<html lang="pt-BR">`, a busca com `language: 'portuguese'` e um dicionário de tradução da interface em `components/provider.tsx`, cobrindo as chaves do Fumadocs e do fumadocs-openapi ("Buscar", "Nesta página", "Testar no playground", "Enviar"…). Chave sem tradução cai no inglês.
4. **Conteúdo** — tirado das fontes que já existiam, para não ter duas versões da verdade:
   - **Manual de uso**: `docs/user-manual.md` dividido em 14 páginas (uma por seção). Na conversão, `<https://…>` virou link Markdown e `{…}` fora de código foi escapado — no MDX as duas coisas quebram.
   - **API REST**: páginas escritas para o site, com exemplos em abas cURL / Go / Python / TypeScript conferidos contra os SDKs gerados.
   - **SDKs**: a partir de `sdk/README.md`.
   - **Agentes de IA**: a tabela de ferramentas sai do `catalog.go` do servidor MCP.
   - **Self-hosting**: seções do README principal (instalação, configuração, segurança, e-mail, deploy, testes).
5. **Playground** — `lib/openapi.ts` (`createOpenAPI`, só no build), `components/api-page.tsx` (`createOpenAPIPage`, componente de cliente) e `scripts/generate-api-pages.ts` (`generateFiles`), que gera `content/docs/api/endpoints/*.mdx` do contrato a cada build. O *proxy* do fumadocs-openapi **não** é usado: ele precisa de servidor, e o site é estático. O navegador chama a API direto, e a API libera essa origem por CORS (`NFSE_CORS_ORIGINS` no painel).
6. **Referência** — `app/api-reference/` monta o componente React do Scalar com telemetria, "Ask AI" e barra de desenvolvedor desligados (os três mandariam o contrato para a nuvem do Scalar).
7. **Contrato** — o mesmo `internal/adapters/web/api/openapi.json` do painel, copiado para `public/openapi.json` no build com o `servers` trocado pela URL de produção (`NFSE_DOCS_API_URL` troca, para testar contra um painel local).

## Estrutura

```
site/
├── app/
│   ├── (home)/page.tsx            página inicial (cartões das seções)
│   ├── docs/layout.tsx            layout notebook
│   ├── docs/[[...slug]]/page.tsx  página de doc (+ OpenAPIPage nas operações)
│   ├── api-reference/             Scalar
│   ├── api/search/route.ts        índice da busca (estático)
│   ├── llms.txt · llms-full.txt · llms.mdx/   versões em texto para agentes
│   └── og/                        imagem de compartilhamento por página
├── components/                    provider (pt-BR, busca), mdx, api-page, search
├── content/docs/
│   ├── manual/  api/  sdks/  agents/  self-hosting/   uma aba cada
│   └── api/endpoints/             GERADO do contrato — não editar
├── lib/                           source (loader), openapi, shared, layout
└── scripts/generate-api-pages.ts  gera api/endpoints
```

Medido no último build: **48** páginas `.mdx` (9 geradas do contrato) → **52** HTML no export; `out/` ocupa 35 MB, dos quais 23 MB são o JavaScript do Next/Scalar e 2,2 MB as imagens de compartilhamento.

## Rodar, gerar e publicar

Tudo passa por [`scripts/docs-site.sh`](../scripts/docs-site.sh), chamado pelas tasks da raiz:

| Task | O que faz |
|---|---|
| `task docs:dev` | servidor de desenvolvimento em http://localhost:3000 |
| `task docs:build` | contrato → `public/` · gera as páginas de operação · `next build` → `out/` |
| `task docs:deploy` | build + Cloudflare Pages (cria o projeto `nfse-docs` se não existe) + domínio `nfse-docs.bassi.engineer` (CNAME com proxy para `nfse-docs.pages.dev`) |

O deploy lê o token do Cloudflare do `pass` (`bassi/cloudflare/token`), passado ao `curl` pelo stdin e ao `wrangler` por variável de ambiente — nunca na linha de comando.

Para ver o export como em produção: `npx serve out -l 3007`.

## Escrever uma página

- Crie `content/docs/<aba>/<nome-em-ingles>.mdx` com `title` e `description` **entre aspas** no frontmatter (dois-pontos sem aspas quebram o YAML) e acrescente o nome no `meta.json` da aba.
- Componentes disponíveis: `Callout`, `Cards`/`Card`, `Tabs`/`Tab`, `Steps`/`Step`, blocos de código com título.
- `{` e `}` fora de bloco de código viram expressão JavaScript no MDX: escreva dentro de crase.
- Mudou a API? Atualize o `openapi.json` — as páginas de `api/endpoints` saem dele no próximo build.

## Armadilhas que já custaram tempo

| Sintoma | Causa | Correção |
|---|---|---|
| `YAMLParseError: Nested mappings are not allowed` | título com dois-pontos sem aspas no frontmatter | aspas em todo `title`/`description` |
| `Please use <DocsPage /> under <DocsLayout />` | página importando de `layouts/docs/page` com o layout `notebook` | importar de `fumadocs-ui/layouts/notebook/page` |
| `TS5097: import path can only end with '.ts'` no `next build` | o script de geração (rodado pelo Node) entrava na checagem de tipos do Next | `scripts` no `exclude` do `tsconfig.json` |
| playground: *"Failed to fetch … a proxy is needed"* | a API não liberava a origem da documentação | `NFSE_CORS_ORIGINS` no painel; o proxy do Fumadocs exige servidor |

O playground guarda no navegador o token digitado: em computador compartilhado, use um token que possa revogar (Integrações → Tokens de API).
