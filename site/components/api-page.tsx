'use client';
import { createOpenAPIPage } from 'fumadocs-openapi/ui';

// Página de operação da API com o playground: token, parâmetros, corpo e o
// botão de enviar, chamando a API direto do navegador (a API libera esta
// origem por CORS — NFSE_CORS_ORIGINS no painel).
export const OpenAPIPage = createOpenAPIPage();
