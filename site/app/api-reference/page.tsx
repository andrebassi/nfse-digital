import type { Metadata } from 'next';
import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { baseOptions } from '@/lib/layout.shared';
import { Reference } from './reference';

// Título da aba do navegador para a referência.
export const metadata: Metadata = {
  title: 'Referência da API',
  description: 'Todas as rotas, parâmetros, respostas e exemplos da API REST do nfse-digital.',
};

// Página /api-reference: o topo do site (mesma navegação das outras páginas) com
// o Scalar ocupando o resto.
export default function ReferencePage() {
  return (
    <HomeLayout {...baseOptions()}>
      <Reference />
    </HomeLayout>
  );
}
