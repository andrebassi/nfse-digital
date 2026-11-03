'use client';
import SearchDialog from '@/components/search';
import { RootProvider } from 'fumadocs-ui/provider/next';
import { type ReactNode } from 'react';

// Tradução da interface do Fumadocs para pt-BR. As chaves são as do pacote
// (texto em inglês + contexto entre parênteses); a que faltar cai no inglês.
const ptBR: Record<string, string> = {
  'Search(search trigger)': 'Buscar',
  'Search(search dialog)': 'Buscar na documentação',
  'No results found(search dialog)': 'Nada encontrado',
  'Search failed(search dialog)': 'A busca falhou',
  'On this page(table of contents)': 'Nesta página',
  'No Headings(table of contents)': 'Sem seções',
  'Table of Contents(inline table of contents)': 'Sumário',
  'Next Page(pagination)': 'Próxima',
  'Previous Page(pagination)': 'Anterior',
  'Edit on GitHub(edit page)': 'Editar no GitHub',
  'Last updated on(page footer)': 'Atualizado em',
  'Copy Markdown(page actions)': 'Copiar Markdown',
  'Copied Markdown(page actions)': 'Markdown copiado',
  'View as Markdown(page actions)': 'Ver como Markdown',
  'Open in GitHub(page actions)': 'Abrir no GitHub',
  'Open in ChatGPT(page actions)': 'Abrir no ChatGPT',
  'Open in Claude(page actions)': 'Abrir no Claude',
  'Open in Cursor(page actions)': 'Abrir no Cursor',
  'Open(page actions)': 'Abrir',
  'Read {url}, I want to ask questions about it.(page actions)': 'Leia {url}, quero fazer perguntas sobre isso.',
  'Theme(site menu)': 'Tema',
  'Page Not Found(404 not found page)': 'Página não encontrada',
  'Back to Home(404 not found page)': 'Voltar ao início',
  'The page you are looking for might have been removed, had its name changed, or is temporarily unavailable.(404 not found page)':
    'A página pode ter sido removida, mudado de nome ou estar indisponível.',
  'Hide Sidebar(sidebar)': 'Esconder menu',
  'Show Sidebar(sidebar)': 'Mostrar menu',
  'Type(type table)': 'Tipo',
  'Default(type table)': 'Padrão',
  // Páginas de operação e playground (fumadocs-openapi).
  'Try in Playground(playground)': 'Testar no playground',
  'Send(playground)': 'Enviar',
  'Sending request...(playground)': 'Enviando…',
  'Send a request to see its response.(playground)': 'Envie para ver a resposta.',
  'Request(playground)': 'Requisição',
  'Response(playground)': 'Resposta',
  'Body(playground)': 'Corpo',
  'Query(playground)': 'Query',
  'Path(playground)': 'Caminho',
  'Header(playground)': 'Cabeçalho',
  'Authorization(playground)': 'Autorização',
  'Authorization(operation page)': 'Autorização',
  'Authorization(security scheme)': 'Autorização',
  'Bearer <token>(security scheme)': 'Bearer <token>',
  'Enter value(playground)': 'Digite o valor',
  'Close(playground)': 'Fechar',
  'Close(operation page)': 'Fechar',
  'This endpoint has no parameters.(playground)': 'Esta rota não tem parâmetros.',
  'No example available.(playground)': 'Sem exemplo.',
  'Not set(playground)': 'Vazio',
  'Request Failed(playground result display)': 'A requisição falhou',
  'Check the server URL. If the API does not allow cross-origin requests from this site, a proxy is needed.(playground result display)':
    'Confira o servidor. Se a API não liberar chamadas vindas deste site (CORS), o navegador bloqueia.',
  'Empty response body(playground result display)': 'Resposta sem corpo',
  'Binary response body, {length} bytes(playground result display)': 'Resposta binária, {length} bytes',
  'Body(playground result display)': 'Corpo',
  'Headers(playground result display)': 'Cabeçalhos',
  'Download(playground result display)': 'Baixar',
  'Clear(playground result display)': 'Limpar',
  'Server URL(playground server select)': 'Servidor',
  'Example Request(operation page)': 'Exemplo de requisição',
  'Example request(operation page)': 'Exemplo de requisição',
  'Example Requests(operation page)': 'Exemplos de requisição',
  'Example Response(operation page)': 'Exemplo de resposta',
  'Request Body(operation page)': 'Corpo da requisição',
  'Response Body(operation page)': 'Corpo da resposta',
  'Response status(operation page)': 'Status da resposta',
  'Query Parameters(operation page)': 'Parâmetros de query',
  'Path Parameters(operation page)': 'Parâmetros de caminho',
  'Header Parameters(operation page)': 'Parâmetros de cabeçalho',
  'Media type(operation page)': 'Tipo de mídia',
  'Schema(operation page)': 'Esquema',
  'Security scheme(operation page)': 'Esquema de segurança',
  'Code language(operation page)': 'Linguagem',
  'Example(operation page)': 'Exemplo',
  'Properties(schema UI)': 'Propriedades',
  'Filter Properties(schema UI)': 'Filtrar propriedades',
  'Example(schema UI)': 'Exemplo',
  'Default(schema UI)': 'Padrão',
  'Format(schema UI)': 'Formato',
  'Items(schema UI)': 'Itens',
  'Length(schema UI)': 'Tamanho',
  'Range(schema UI)': 'Faixa',
};

// Provider do site: busca estática (Orama no navegador) e interface em pt-BR.
export function Provider({ children }: { children: ReactNode }) {
  return (
    <RootProvider search={{ SearchDialog }} i18n={{ locale: 'pt-BR', translations: ptBR }}>
      {children}
    </RootProvider>
  );
}
