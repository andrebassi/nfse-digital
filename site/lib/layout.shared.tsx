import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { appName, gitConfig, panelUrl } from './shared';

// Opções comuns aos layouts: título, links do topo e o GitHub do projeto.
export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <span className="inline-flex items-center gap-2 font-semibold">
          <span className="grid size-6 place-items-center rounded-md bg-fd-foreground text-[11px] text-fd-background">nf</span>
          {appName}
        </span>
      ),
    },
    links: [
      { text: 'Referência da API', url: '/api-reference' },
      { text: 'Abrir o painel', url: panelUrl, external: true },
    ],
    githubUrl: `https://github.com/${gitConfig.user}/${gitConfig.repo}`,
  };
}
