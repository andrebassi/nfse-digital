'use client';
import { ApiReferenceReact } from '@scalar/api-reference-react';
import '@scalar/api-reference-react/style.css';

// Referência navegável da API, renderizada pelo Scalar a partir do contrato
// (o mesmo openapi.json do painel, copiado para public/ no build com o
// servers apontando para a produção). Tema kepler e layout modern, como na
// documentação da Hodle. Sem telemetria, sem "Ask AI" e sem a barra de
// desenvolvedor: os três mandariam o contrato para a nuvem do Scalar.
export function Reference() {
  return (
    <ApiReferenceReact
      configuration={{
        url: '/openapi.json',
        theme: 'kepler',
        layout: 'modern',
        hideClientButton: true,
        showDeveloperTools: 'never',
        telemetry: false,
        agent: { disabled: true },
        withDefaultFonts: false,
        defaultHttpClient: { targetKey: 'shell', clientKey: 'curl' },
        metaData: { title: 'Referência da API · nfse-digital' },
      }}
    />
  );
}
