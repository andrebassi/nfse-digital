import Link from 'next/link';
import { panelUrl } from '@/lib/shared';

// Os cinco caminhos da documentação, um cartão cada (as abas do topo).
const sections = [
  { href: '/docs/manual', title: 'Manual de uso', text: 'Do primeiro acesso à nota no e-mail do cliente, tela por tela.' },
  { href: '/docs/api', title: 'API REST', text: 'Emitir, consultar e cancelar a partir do seu ERP, com idempotência.' },
  { href: '/docs/sdks', title: 'SDKs', text: 'Clientes Go, Python e TypeScript gerados do contrato da API.' },
  { href: '/docs/agents', title: 'Agentes de IA', text: 'Claude Code, Claude Desktop e Codex pelo servidor MCP.' },
  { href: '/docs/self-hosting', title: 'Self-hosting', text: 'Instalar, configurar e publicar a sua instância.' },
  { href: '/api-reference', title: 'Referência da API', text: 'Todas as rotas, parâmetros e respostas, com exemplos.' },
];

// Página inicial: o que é o nfse-digital, os dois botões principais e os cartões.
export default function HomePage() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-12 px-6 py-16">
      <section className="flex flex-col gap-5">
        <p className="text-sm font-medium text-fd-muted-foreground">NFS-e · padrão nacional · SEFIN</p>
        <h1 className="text-4xl font-semibold tracking-tight md:text-5xl">Emita NFS-e direto na SEFIN, pelo painel, pela API ou por um agente.</h1>
        <p className="max-w-2xl text-lg text-fd-muted-foreground">
          O nfse-digital é um emissor de Nota Fiscal de Serviço eletrônica no padrão nacional: painel web, API REST,
          servidor MCP para agentes de IA e CLI, num binário só. Esta é a documentação de uso e de integração.
        </p>
        <div className="flex flex-wrap gap-3">
          <Link href="/docs/manual" className="rounded-lg bg-fd-primary px-4 py-2 text-sm font-medium text-fd-primary-foreground">
            Começar pelo manual
          </Link>
          <Link href="/docs/api" className="rounded-lg border px-4 py-2 text-sm font-medium">
            Integrar pela API
          </Link>
          <a href={panelUrl} className="rounded-lg px-4 py-2 text-sm font-medium text-fd-muted-foreground hover:text-fd-foreground">
            Abrir o painel ↗
          </a>
        </div>
      </section>
      <section className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {sections.map((section) => (
          <Link
            key={section.href}
            href={section.href}
            className="flex flex-col gap-2 rounded-xl border bg-fd-card p-5 transition-colors hover:bg-fd-accent"
          >
            <span className="font-medium">{section.title}</span>
            <span className="text-sm text-fd-muted-foreground">{section.text}</span>
          </Link>
        ))}
      </section>
    </main>
  );
}
