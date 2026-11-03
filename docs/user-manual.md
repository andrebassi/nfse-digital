# Manual de uso — nfse-digital

Emissor de NFS-e pelo padrão nacional (SEFIN / Emissor Nacional), feito para quem emite poucas notas por mês para os mesmos clientes. Este manual segue a ordem em que as coisas acontecem: primeiro acesso, cadastros, emissão, o dia a dia e as integrações.

| Onde abrir | Endereço |
|---|---|
| Painel publicado | <https://nfse.bassi.engineer> |
| Painel na sua máquina | `task serve` → <http://127.0.0.1:8735> |

> **Homologação e produção.** No topo de toda tela, o seletor **HOMOLOGAÇÃO · PRODUÇÃO** diz em que ambiente as notas saem. Em **homologação** a nota é de teste, sem valor fiscal. Em **produção**, cada nota é um documento fiscal de verdade: não dá para desfazer, só cancelar. Toda conta começa em homologação.

---

## Sumário

1. [Primeiro acesso](#1-primeiro-acesso)
2. [Configurar o emitente](#2-configurar-o-emitente)
3. [Tomadores](#3-tomadores)
4. [Modelos de serviço](#4-modelos-de-serviço)
5. [Emitir uma nota](#5-emitir-uma-nota)
6. [Acompanhar as notas](#6-acompanhar-as-notas)
7. [Cancelar uma nota](#7-cancelar-uma-nota)
8. [Enviar a nota por e-mail](#8-enviar-a-nota-por-e-mail)
9. [Agendamentos (nota mensal automática)](#9-agendamentos-nota-mensal-automática)
10. [Passar para produção](#10-passar-para-produção)
11. [Integrações: agentes de IA](#11-integrações-agentes-de-ia)
12. [Integrações: API REST](#12-integrações-api-rest)
13. [Segurança da conta](#13-segurança-da-conta)
14. [Problemas comuns](#14-problemas-comuns)

---

## 1. Primeiro acesso

1. **Criar conta** — em **Crie sua conta**, informe nome, e-mail e senha. No painel publicado só os e-mails liberados pelo dono conseguem criar conta.
2. **Enviar o certificado A1** — até o certificado chegar, o menu fica travado e só a tela do certificado abre (o aviso "Envie o certificado A1 para começar →" leva até ela).
   - Em **Configurações → Certificado**, arraste o arquivo `.pfx` (ou clique para escolher) e digite a **Senha do certificado**.
   - O painel confere a senha, a validade e o CNPJ antes de gravar. Certificado vencido, sem CNPJ ou de outra empresa é recusado.
   - O certificado fica guardado cifrado: o `.pfx` e a senha só existem abertos na memória, na hora de assinar a nota.
3. **Configurar o emitente** — veja a seção 2. Enquanto ele não existe, o painel mostra "Emitente ainda não configurado".
4. **(Opcional) Trazer o que você já emitiu** — veja [Importar notas antigas](#importar-notas-antigas).

## 2. Configurar o emitente

**Configurações → Emitente** (as abas de Configurações são Certificado, Emitente e Segurança):

| Campo | O que pôr |
|---|---|
| Razão social, CNPJ, Inscrição municipal | como no cadastro da prefeitura |
| Município + IBGE | município do estabelecimento e o código IBGE de 7 dígitos |
| Optante pelo Simples | Não optante · MEI · ME/EPP |
| Regime de apuração (SN) | só para optante: federal e municipal pelo SN, federal pelo SN e municipal fora, ou os dois fora |
| Regime especial de tributação | normalmente **Nenhum** |
| Alíquota aproximada do Simples (%) | **obrigatória para ME/EPP**: a alíquota efetiva do DAS; vai na nota como total aproximado de tributos |
| Série da DPS / Próximo nº da DPS | numeração automática; só mude para continuar a sequência de outro emissor |
| Ambiente | homologação ou produção (produção pede o CNPJ de novo — seção 10) |

Clique em **Salvar**. O cartão do emitente aparece no alto do menu lateral.

## 3. Tomadores

São os clientes para quem você emite. Menu **Tomadores**.

- **Novo tomador**: CPF/CNPJ, nome ou razão social, e-mail (opcional), município, CEP e endereço. Marque **Tomador no exterior** só para exportação de serviço.
- CPF/CNPJ repetido é recusado: cada documento é um tomador só por conta.
- A busca filtra por nome ou documento.
- Ao editar um tomador, a tela tem duas abas: **Cadastro** e **Envio por e-mail** (seção 8).

## 4. Modelos de serviço

O modelo guarda o serviço que você presta e como ele é tributado, para a emissão vir preenchida. Menu **Modelos**.

| Campo | Observação |
|---|---|
| Nome do modelo | como você reconhece o serviço ("Suporte mensal") |
| Descrição do serviço | aceita `{mes}` e `{ano}`, trocados pelo mês por extenso e pelo ano da competência ("Suporte técnico - {mes}/{ano}") |
| Código do serviço (tributação nacional) | o código da lista nacional (ex.: 01.07.01) |
| Código municipal, NBS | opcionais |
| Município da prestação + IBGE | onde o serviço é prestado |
| Valor padrão | sugerido na emissão; pode mudar na hora |
| % tributos Simples, Tributação e Retenção do ISSQN | opcionais; vazios, valem os do emitente |
| Tomador padrão | opcional: o modelo já escolhe o cliente |
| IBS/CBS (reforma tributária) | indicador de operação, CST e classificação, quando exigidos |

### Importar notas antigas

Há dois jeitos de transformar notas já emitidas em cadastro (tomador + modelo + histórico):

- **Importar notas do Portal Nacional** (Modelos → Importar notas do Portal Nacional): consulta as NFS-e já emitidas pelo CNPJ no Portal Nacional, usando o certificado enviado. É só leitura: nenhuma nota é emitida nem alterada. Antes, libere em **Configurações → Certificado → Consulta ao Portal Nacional → Autorizar consulta** (dá para revogar ali mesmo).
- **Importar de um XML** (Modelos → botão **Importar de um XML**): envie o XML de uma NFS-e baixado do Portal Nacional.

## 5. Emitir uma nota

Menu **Emitir nota**:

1. **Tomador** — busque e escolha o cliente.
2. **Serviço e valor** — escolha o **modelo** (o valor e a descrição vêm preenchidos) e confira **Valor (R$)**, **Competência** (o mês a que o serviço se refere) e **Descrição do serviço**.
3. Confira o **Resumo** à direita e clique em **Emitir nota**.
   - Em produção, marque a caixa de confirmação: sem ela, nada é enviado à SEFIN.

O resultado aparece no detalhe da nota:

| Estado | Significado |
|---|---|
| **autorizada** | a SEFIN aceitou; a nota tem número e chave de acesso |
| **rejeitada** | a SEFIN recusou; o motivo aparece traduzido em "Retorno da SEFIN". Corrija o cadastro e emita de novo |
| **pendente** | a SEFIN não deu resposta definitiva (rede, instabilidade). Não emita outra: use **Sincronizar com a SEFIN** ou espere o reenvio |
| **cancelada** | cancelada na SEFIN (seção 7) |

Se o tomador tem o **envio automático** ligado (seção 8), a nota autorizada já sai por e-mail.

## 6. Acompanhar as notas

Menu **Notas**:

- **Período** (últimos 7, 30, 90 ou 365 dias) + **Filtrar**, e as abas **Todas · Autorizadas · Canceladas · Rejeitadas**, com a contagem.
- Os indicadores do alto: receita, autorizadas, canceladas e ticket médio do período.
- Clique numa nota para abrir o **detalhe**: tomador, competência, série/DPS, serviço, ambiente, chave de acesso e descrição. No detalhe:
  - **Baixar DANFSe (PDF)** — o documento auxiliar, com QR Code para a consulta pública (nota autorizada ou cancelada);
  - **Enviar por e-mail** e o histórico de envios (seção 8);
  - **Cancelar nota** (seção 7).
- **Sincronizar com a SEFIN** alinha o estado das notas daqui com o da SEFIN (útil para notas pendentes).
- O **ícone de olho** no topo esconde os valores da tela, para mostrar o painel a alguém.

## 7. Cancelar uma nota

No detalhe de uma nota **autorizada**, abra **Cancelar nota**:

1. **Motivo**: Erro na emissão · Serviço não prestado · Outros.
2. **Justificativa**: de 15 a 255 caracteres; vai para a SEFIN.
3. Em produção, marque "Confirmo o cancelamento em produção".
4. **Cancelar na SEFIN**.

Cancelar é irreversível. Passado o prazo do município, a SEFIN recusa e o cancelamento só sai pela prefeitura do município de incidência — a mensagem de retorno avisa.

## 8. Enviar a nota por e-mail

A nota chega ao tomador com o DANFSe (PDF) e o XML, saindo do **seu Gmail**.

### 8.1 Gerar a senha de app do Google (uma vez)

1. Ative a **verificação em duas etapas** na conta Google.
2. Em <https://myaccount.google.com/apppasswords>, crie uma senha de app (nome sugerido: "nfse-digital").
3. O Google mostra **16 letras** em quatro blocos (`abcd efgh ijkl mnop`). Pode colar com ou sem os espaços. **Não** é a senha normal da conta.

Para cortar o acesso depois, basta revogar essa senha de app na mesma página do Google.

### 8.2 Configurar o tomador

**Tomadores → (tomador) → aba Envio por e-mail**:

| Campo | Regra |
|---|---|
| Enviar automaticamente cada nota autorizada | ligado, a nota sai sozinha logo depois de autorizada; desligado, só pelo botão da nota |
| Gmail que envia · Nome do remetente | um remetente por conta, **vale para todos os tomadores** |
| Senha de app do Google | testada com um login no Gmail **ao salvar** — senha errada não é gravada. Fica guardada cifrada e nunca volta para a tela; para manter a guardada, deixe o campo vazio |
| Para · Cópia | vários e-mails separados por vírgula |
| Assunto · Mensagem | texto livre com os marcadores abaixo |
| Anexar PDF (DANFSe) · Anexar XML | os anexos do e-mail |

**Marcadores** — clique num deles para inserir onde está o cursor:

| Marcador | Vira | Exemplo |
|---|---|---|
| `{numero}` | número da NFS-e | 123 |
| `{valor}` | valor | R$ 1.500,00 |
| `{competencia}` | competência | 11/2026 |
| `{mesCompetencia}` | competência por extenso | novembro/2026 |
| `{dataEmissao}` | data de emissão | 02/11/2026 |
| `{tomador}` | nome do tomador | AURORA SISTEMAS LTDA |
| `{emitente}` | razão social do emitente | |
| `{cnpjEmitente}` | CNPJ do emitente | 11.222.333/0001-81 |
| `{descricao}` | descrição do serviço | |
| `{chave}` | chave de acesso | 50 dígitos |
| `{linkConsulta}` | link da consulta pública da nota | https://www.nfse.gov.br/ConsultaPublica/?tpc=1&chave=… |

A **Prévia**, à direita, acompanha o que você digita, com uma nota de exemplo (nº 123, R$ 1.500,00).

### 8.3 Enviar um teste

Em **Enviar e-mail de teste**, ponha o seu endereço e clique em **Enviar teste**:

- sai pelo Gmail da tela, com o texto da tela **mesmo sem salvar**;
- assunto com **[TESTE]** e sem cópia;
- os anexos são de exemplo, marcados **"SIMULAÇÃO · SEM VALOR FISCAL"** (no PDF, em diagonal; no XML, na primeira linha);
- nada é salvo.

Depois do teste, clique em **Salvar envio por e-mail**.

### 8.4 No dia a dia

- **Automático**: com a caixa ligada, cada nota autorizada do tomador sai sozinha — emitida pelo painel, pela API, pelo agente ou pelo agendamento.
- **Manual**: no detalhe da nota autorizada, **Enviar por e-mail** manda agora para o Para e a Cópia do tomador.
- **Histórico**: o detalhe da nota lista cada envio — *Enviada* ou *Falhou*, quando, automático ou manual, para quem — e o motivo da falha.
- **Se o Gmail falhar**, a nota continua autorizada: a falha só fica no histórico. Corrija (senha revogada, destinatário errado) e use o botão manual.

## 9. Agendamentos (nota mensal automática)

Menu **Agendamentos → Novo agendamento**, para emitir a mesma nota todo mês:

| Campo | Regra |
|---|---|
| Modelo de serviço · Tomador · Valor padrão | o que sai todo mês |
| Dia da emissão | todo dia N; de 29 a 31, cai no último dia nos meses mais curtos |
| Competência | **mês anterior** (o serviço do mês que passou) ou **mês da emissão** (serviço concluído no próprio dia) |
| Descrição | aceita `{mes}` e `{ano}` |
| Aviso na véspera | aviso no celular (ntfy) para confirmar, mudar o valor ou pular o mês; sem resposta, emite com o valor vigente |
| Enviar a nota ao tomador após a emissão | manda por e-mail com o Gmail e o texto da aba Envio por e-mail do tomador. Se o tomador já tem o envio automático, sai **um** e-mail só |

**Pré-visualizar** mostra as próximas datas de emissão e de aviso. A emissão acontece às **03:00 (Brasília)**. Se a SEFIN estiver fora, novas tentativas de hora em hora até o fim do dia, com o mesmo número de DPS (nunca duas notas).

> Na sua máquina, o agendador só roda com `nfse serve --run-schedules`. No painel publicado ele já está ligado.

**Em produção**, um agendamento só emite sozinho se a permissão de produção foi gravada nele — e o formulário do painel **não grava essa permissão**: ela só é dada pela linha de comando (`nfse schedule add --production`). Agendamento criado pelo painel, com o emitente em produção, fica bloqueado e não emite.

## 10. Passar para produção

Clique em **PRODUÇÃO** no seletor do topo (ou **Configurações → Ambiente**):

1. Leia o aviso: em produção cada nota é um documento fiscal de verdade.
2. Para confirmar, digite o **CNPJ do emitente**.
3. **Passar para produção**.

A partir daí, toda emissão e todo cancelamento — no painel, na API e pelo agente — exigem confirmação explícita. Para voltar a testar, clique em **HOMOLOGAÇÃO**.

## 11. Integrações: agentes de IA

Menu **Integrações → Conectar agente**: emita, consulte e cancele conversando com um agente.

| Agente | Como liga |
|---|---|
| **Claude Code** | **Gerar token** (aparece uma única vez) e rodar o comando `claude mcp add --transport http nfse-digital …` que a tela monta com o token |
| **Claude Desktop** | trecho para o arquivo `claude_desktop_config.json`, mostrado na tela (servidor local, sem token) |
| **Codex** | `export NFSE_TOKEN=…` + `codex mcp add nfse-digital --url …`, mostrado na tela |
| ChatGPT · Grok | conectam a partir da nuvem deles e precisam de uma URL pública; a tela explica |

Depois, opcionalmente, **Baixar skill.md** e pôr na pasta de skills do agente: ela ensina o agente a confirmar com você antes de emitir e a nunca emitir em dobro.

Para conferir: peça ao agente *"qual o status da minha conta no nfse?"* e clique em **Testar conexão**.

Outras abas: **Tokens de API** (listar e revogar), **Agentes conectados** (quem falou por último e com qual ferramenta) e **Ferramentas** (o que o agente pode fazer; as marcadas **SEFIN** agem em seu nome e, em produção, exigem `confirm_production`).

## 12. Integrações: API REST

**Integrações → API REST**, para o seu ERP ou sistema de faturamento emitir sem agente no meio.

- **URL base**: `<endereço do painel>/api/v1` · **Autenticação**: `Authorization: Bearer <TOKEN>` (o mesmo token do MCP; **Gerar token** na própria aba).
- Abas **Emitir · Consultar · Listar · Cancelar** com o `curl` pronto e o botão **Copiar**.
- **Idempotência**: toda emissão leva uma `idempotency_key` sua — use o id da fatura. Reenviar a mesma chave nunca emite outra nota, devolve a mesma. Consulte a nota pela chave (`/invoices/by-key?idempotency_key=…`) ou pelo id.
- Tomadores e modelos continuam sendo cadastrados no painel; a API só os lista.
- **Documentação da API** (referência navegável com exemplos em curl, Node.js e Python), **OpenAPI** (contrato para importar no Postman/Insomnia) e **llms.txt** (o guia para o agente do seu sistema integrar sozinho) ficam nos botões da aba.

## 13. Segurança da conta

**Configurações → Segurança**:

- **Passkeys**: entre com Touch ID/Face ID em vez de senha (**Entrar com passkey** na tela de login). Na sua máquina, a passkey só funciona pelo endereço `localhost` — o padrão WebAuthn não aceita IP. **Remover passkeys** volta o login para só senha.
- **Tokens de API**: revogue em **Integrações → Tokens de API** o que não usa mais; o token para de funcionar na hora.
- **Senha de app do Gmail**: revogue no Google quando quiser cortar o envio por e-mail.
- **Excluir conta e todos os dados**: apaga emitente, tomadores, modelos, notas, agendamentos, o certificado guardado, tokens, passkeys e a configuração de e-mail. As notas continuam na SEFIN; só a cópia daqui some. Para confirmar, digite o e-mail da conta. Não tem volta.

## 14. Problemas comuns

| Sintoma | Causa provável | O que fazer |
|---|---|---|
| Menu travado, só abre o certificado | conta sem certificado A1 | Configurações → Certificado |
| "Emitente ainda não configurado" | emitente sem CNPJ | Configurações → Emitente |
| Nota **rejeitada** | a SEFIN recusou um dado (código, município, alíquota) | leia o "Retorno da SEFIN" traduzido, corrija e emita de novo |
| Nota **pendente** | a SEFIN não respondeu | não emita de novo; **Sincronizar com a SEFIN** |
| Emissão em produção recusada | falta a confirmação | marque a caixa (painel) ou mande `confirm_production: true` (API, agente) |
| "o Gmail recusou o login" | senha normal no lugar da senha de app, ou senha revogada | gere outra em myaccount.google.com/apppasswords (seção 8.1) |
| E-mail não chegou | destinatário errado, spam ou falha registrada | veja o histórico no detalhe da nota; confira o Para; use **Enviar por e-mail** |
| Cancelamento recusado por prazo | passou o prazo do município | peça o cancelamento à prefeitura do município de incidência |
| Passkey não aparece | painel aberto por IP | abra por `http://localhost:8735` |
| Agente não conecta | token revogado ou comando antigo | gere token novo e rode o comando da tela de novo; **Testar conexão** |

---

Documentação técnica: [README](../README.md) · [PRD](prd.md) · [Arquitetura](architecture.md) · [Notas medidas da SEFIN](sefin-notes.md).
