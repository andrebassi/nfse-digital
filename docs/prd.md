# PRD — nfse-digital: emissor pessoal de NFS-e (padrão nacional)

## 1. Goals and Background Context

### Goals

- Emitir NFS-e do CNPJ do André (EXEMPLO TECNOLOGIA LTDA, 11.222.333/0001-81) direto na API da SEFIN Nacional, sem portal do governo e sem SaaS de terceiro.
- Reaproveitar uma nota já emitida (XML) como modelo: emitente, tomador e serviço saem do arquivo, e a próxima nota exige só valor e competência.
- Três formas de uso sobre o mesmo núcleo: CLI (`nfse`), servidor MCP (pedir a nota ao Claude) e painel web local.
- Certificado A1 e senha só no `pass`, nunca em disco em claro.
- Homologação como padrão; produção só com ação explícita.

### Background Context

Hoje o André usa um SaaS de emissão ( R$ 35/mês depois de 05/11/2026), que emite pela API nacional. A única nota do período (IPÊ INFORMÁTICA LTDA, R$ 19.000,00, 28/10/2026) foi rejeitada com
`[E0039] O município emissor informado na DPS deve estar parametrizado para utilizar os emissores públicos nacionais`. O emitente fica em Barueri (IBGE `3505708`, conferido em `servicodados.ibge.gov.br`), município que manteve emissor próprio.

**Decisão do dono (29/10/2026): alvo é só a API nacional.** Atualização medida no mesmo dia (story 2.1, `docs/sefin-notes.md`): as notas nº 37, 38 e 39 de 05/10/2026 foram emitidas pelo próprio Emissor Nacional com município emissor Barueri, em produção. Barueri usa o emissor nacional em produção; o E0039 vem só da produção restrita (homologação). O produto trata o E0039 da homologação como estado conhecido, e a primeira autorização real acontece em produção, com a guarda da story 4.1.

O projeto é de uso pessoal, roda só na máquina do André, sem multiusuário, sem cobrança e sem cópia de marca ou layout de outro produto — a referência é funcional.

### Change Log

| Date | Version | Description | Author |
|---|---|---|---|
| 2026-10-29 | 0.1 | Versão inicial | @pm |
| 2026-10-29 | 0.2 | Protótipo das telas vira o Epic 1; demais epics descem um número | @pm |

## 2. Requirements

### Functional

- **FR1** — Carregar o certificado A1 (.pfx) e a senha a partir do `pass` (`bassi/nfse/cert-a1-pfx` em base64, `bassi/nfse/cert-a1-password`), validar CNPJ e validade, e avisar quando faltar menos de 30 dias para vencer.
- **FR2** — Cadastrar e editar o emitente: razão social, CNPJ, inscrição municipal, município (IBGE), opção pelo Simples, regime de apuração, regime especial, série e próximo número da DPS.
- **FR3** — Importar o XML de uma NFS-e já emitida e extrair emitente, tomador e serviço (código de tributação nacional, descrição, alíquota) como modelo reutilizável.
- **FR4** — CRUD de tomadores (CNPJ/CPF, razão social, endereço, e-mail) e de modelos de serviço.
- **FR5** — Montar a DPS (Declaração de Prestação de Serviço) a partir de modelo + tomador + valor + competência, com numeração automática e sem repetir número.
- **FR6** — Assinar a DPS (XMLDSig enveloped) com o certificado A1 e enviar à SEFIN Nacional por mTLS (`POST /nfse`, corpo `{"dpsXmlGZipB64": ...}`).
- **FR7** — Persistir cada tentativa com estado (`pending`, `authorized`, `rejected`, `cancelled`), XML enviado, XML de retorno, chave de acesso e código/mensagem de erro.
- **FR8** — Traduzir o código de rejeição em mensagem acionável; E0039 com texto fixo explicando que o município não aderiu ao emissor nacional.
- **FR9** — Cancelar nota autorizada por evento (`POST /nfse/{chave}/eventos`, evento e101101) com motivo.
- **FR10** — Baixar o DANFSe (PDF) de nota autorizada.
- **FR11** — Consultar uma nota pela chave e sincronizar o estado local.
- **FR12** — Agendamentos: emitir nota recorrente (ex.: todo dia 1, modelo X, tomador Y, valor Z), executados pelo processo `nfse serve`.
- **FR13** — CLI: `emit`, `list`, `show`, `cancel`, `danfse`, `import`, `customer`, `template`, `schedule`, `cert`, `serve`, `mcp`.
- **FR14** — Servidor MCP (stdio) com tools: listar notas, mostrar nota, emitir, cancelar, listar tomadores e modelos.
- **FR15** — Painel web local: lista de notas com filtros (período, estado), KPIs (receita, autorizadas, canceladas, ticket médio), detalhe com retorno da SEFIN, formulário de emissão e telas de cadastro.

### Non Functional

- **NFR1** — Homologação é o ambiente padrão. Produção exige a flag `--production` na CLI, o parâmetro `confirm_production: true` no MCP e confirmação explícita no painel.
- **NFR2** — Painel escuta só em `127.0.0.1`, confere o header `Host` (contra DNS rebinding) e usa token CSRF em todo POST.
- **NFR3** — Certificado e senha só na memória do processo; nenhum log imprime XML assinado com dado sensível nem senha.
- **NFR4** — Código em Go, arquitetura hexagonal; identificadores em inglês, comentários e UI em pt-BR acentuado.
- **NFR5** — Cobertura ≥ 90% em linha e branch com gate que reprova (`task test:cov`); domain 100%.
- **NFR6** — Binário único, sem dependência de runtime; banco SQLite local em `~/.local/share/nfse/nfse.db`.
- **NFR7** — Painel sem JavaScript próprio: HTML renderizado no servidor (`html/template`) e formulários comuns.
- **NFR8** — Emissão idempotente: reenviar após timeout não pode gerar duas notas (mesmo número de DPS; 409 da SEFIN tratado como "já existe").

## 3. Technical Assumptions

### Repository Structure

Monorepo único em `~/works/labs/nfse-digital` (subdiretório do repo `labs`). Binário chamado `nfse`.

### Service Architecture

Monólito modular hexagonal: um binário com três adapters de entrada (CLI, MCP, HTTP) sobre os mesmos casos de uso.

### Testing Requirements

Unitário em domain e application; adapters testados com `httptest` (servidor TLS fake da SEFIN), SQLite em memória e certificado de teste gerado no próprio teste. Um smoke test manual contra a homologação real, cujo resultado esperado para o emitente de Barueri é **E0039**.

### Additional Technical Assumptions

- A documentação oficial do Sistema Nacional NFS-e tem Swaggers divergentes; o layout de DPS e o algoritmo de assinatura são confirmados contra o XML real de uma nota já emitida (story 2.1) e o XSD oficial.
- Ambiente local por Nix (`flake.nix`), sem Docker.

## 4. Epic List

1. **Epic 1 — Protótipo das telas**: painel local navegável com dados reais e emissor simulado, para validar o fluxo antes de qualquer chamada à SEFIN.
2. **Epic 2 — Emissão em homologação**: certificado, DPS, XML assinado, cliente mTLS, SQLite e CLI, até o retorno real da SEFIN.
3. **Epic 3 — Ciclo de vida da nota**: importar (XML e Portal Nacional), cancelar, DANFSe, consulta e agendamentos reais.
4. **Epic 4 — Interfaces e guarda**: guarda de produção, servidor MCP e o painel ligado ao núcleo real.

## 5. Epic Details

### Epic 1 — Protótipo das telas

Goal: o dono navega por todas as telas do emissor, com os próprios tomadores e modelos, e emite contra um emissor simulado.

- **1.1 Scaffold** — projeto Go hexagonal, Nix, Taskfile e gate de cobertura que reprova.
- **1.2 Domínio do protótipo** — dinheiro, documentos, estados, indicadores, calendário de agendamento, descrição com variáveis, tradução de erro SEFIN.
- **1.3 Repositório em memória** — port `Repository` e seed com os dados reais.
- **1.4 Painel: estrutura, segurança e Notas** — 127.0.0.1, Host check, CSRF, lista, KPIs, detalhe, ocultar valores.
- **1.5 Painel: Emitir nota** — fluxo completo com emissor simulado (E0039 para Barueri).
- **1.6 Painel: Tomadores e Modelos** — cadastros com validação.
- **1.7 Painel: Agendamentos, Configurações e Integrações** — próximas emissões calculadas, emitente editável, instruções de MCP.
- **1.8 Painel: Entrar e Criar conta** — conta única, senha com bcrypt, sessão de 12 h, bloqueio após 5 erros.

Fora do escopo do protótipo, por ser emissor de um usuário só: Assinatura, Equipe e DAS.

### Epic 2 — Emissão em homologação

- **2.1 Spike de layout** — XSD oficial e XML real confirmam `Id`, namespace e assinatura (`docs/sefin-notes.md`).
- **2.2 Certificado A1** — carregado do `pass` para a memória; aviso de vencimento.
- **2.3 Domínio DPS** — estende o domínio do Epic 1 com `DPSId` e campos fiscais.
- **2.4 XML e assinatura** — XML válido no XSD e assinatura verificável por ferramenta independente.
- **2.5 Cliente SEFIN** — mTLS, gzip+base64, 201/400/409, E0039 traduzido.
- **2.6 Persistência** — SQLite substitui o repositório em memória.
- **2.7 CLI de emissão** — `nfse emit/list/show`; smoke test real devolve E0039.

### Epic 3 — Ciclo de vida da nota

- **3.1 Importar** — XML e consulta só leitura ao Portal Nacional, agrupando notas em serviços recorrentes.
- **3.2 Cancelamento** — evento e101101.
- **3.3 DANFSe** — download do PDF.
- **3.4 Consulta e sincronização** — estado local alinhado pela chave.
- **3.5 Agendamentos** — emissão recorrente real, aviso na véspera, nova tentativa a cada hora.

### Epic 4 — Interfaces e guarda

- **4.1 Guarda de produção** — CLI, MCP, painel e agendamento exigem confirmação explícita.
- **4.2 Servidor MCP** — tools de leitura e emissão.
- **4.3 Painel web** — o painel do Epic 1 ligado ao núcleo real.
- **4.4 Passkey** — entrar com Touch ID/chave de segurança (WebAuthn, JavaScript de página).

### Grafo de dependências

```
1.1 → 1.2 → 1.3 → 1.4 → 1.5, 1.6, 1.7, 1.8
2.1 → 2.2 ─┐
1.2 → 2.3 ─┼→ 2.4 → 2.5 ─┐
1.3 → 2.6 ─┴─────────────┴→ 2.7 → 3.1, 3.2, 3.3, 3.4 → 3.5 → 4.1 → 4.2, 4.3
```

1.5, 1.6 e 1.7 podem correr em paralelo depois de 1.4. O Epic 2 depende do domínio (1.2) e do port de repositório (1.3) do Epic 1.

## 6. Next Steps

### Architect Prompt

Desenhar a arquitetura hexagonal em Go do emissor `nfse` a partir deste PRD: portas para certificado, SEFIN, emissor, repositório e relógio; três adapters de entrada; SQLite; mTLS com A1 vindo do `pass`.

### UX Expert Prompt

Painel local sem JavaScript próprio, legível, com o retorno da SEFIN em destaque no detalhe da nota e a faixa de ambiente (homologação/produção) sempre visível. Referência funcional nos prints dos prints de referência (privados, fora do repositório); identidade visual própria.
