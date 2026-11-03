// Código de página (roda no navegador, não no Node): monta a documentação da
// API REST com o Scalar vendorizado em /static/vendor. Fica em arquivo, e não
// inline no HTML, porque a CSP da página recusa script inline.
(() => {
  // A referência lê o contrato servido pelo próprio painel; o servers do
  // contrato já aponta para este host, então "Testar requisição" chama aqui.
  Scalar.createApiReference("#app", {
    url: "/api/v1/openapi.json",
    // Sem fontes do CDN do Scalar: a CSP só aceita fonte deste host.
    withDefaultFonts: false,
    // Sem telemetria e sem o proxy do Scalar: a chamada de teste vai direto
    // para este host, e o token digitado não passa por terceiro.
    telemetry: false,
    proxyUrl: "",
    hideClientButton: true,
    // Sem a barra de desenvolvedor (Share, Deploy API, Generate SDK) que o
    // Scalar mostra em localhost, e sem o "Ask AI": os dois mandam o contrato
    // para a nuvem do Scalar.
    showDeveloperTools: "never",
    agent: { disabled: true },
    defaultHttpClient: { targetKey: "shell", clientKey: "curl" },
    metaData: { title: "API REST · nfse-digital" },
  });
})();
