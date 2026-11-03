import { source } from '@/lib/source';
import { DocsLayout } from 'fumadocs-ui/layouts/notebook';
import { baseOptions } from '@/lib/layout.shared';

// Layout notebook (o mesmo da documentação da Hodle): as pastas raiz do
// conteúdo viram abas no topo — Manual, API REST, SDKs, Agentes, Self-hosting.
export default function Layout({ children }: LayoutProps<'/docs'>) {
  return (
    <DocsLayout tree={source.getPageTree()} {...baseOptions()} tabMode="navbar">
      {children}
    </DocsLayout>
  );
}
