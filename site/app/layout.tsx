import { Inter } from 'next/font/google';
import { Provider } from '@/components/provider';
import './global.css';
import type { Metadata } from 'next';

const inter = Inter({
  subsets: ['latin'],
});

// Título padrão e modelo dos títulos de página, em pt-BR.
export const metadata: Metadata = {
  title: { default: 'nfse-digital — documentação', template: '%s · nfse-digital' },
  description: 'Manual, API REST, SDKs e agentes de IA do nfse-digital, emissor de NFS-e no padrão nacional.',
};

// Layout raiz: fonte, idioma e o provider do Fumadocs (tema, busca, traduções).
export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html lang="pt-BR" className={inter.className} suppressHydrationWarning>
      <body className="flex flex-col min-h-screen">
        <Provider>{children}</Provider>
      </body>
    </html>
  );
}
