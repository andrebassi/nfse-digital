---
name: nfse-digital
description: Emitir, consultar e cancelar NFS-e do André pelo servidor MCP nfse-digital (padrão nacional, SEFIN). Use quando ele pedir para emitir nota, ver notas, baixar DANFSe, cancelar nota ou conferir agendamentos.
---

# Emitir NFS-e com o nfse-digital

O servidor MCP `nfse-digital` (`bin/nfse mcp`, registrado em `.mcp.json`) fala com a SEFIN
nacional usando o certificado A1 do `pass`. Toda chamada que assina pede aprovação no celular
do dono — se a ferramenta demorar, é a aprovação pendente; avise e espere.

Uma nota emitida em produção é documento fiscal de verdade: **não dá para desfazer, só cancelar**.
Trate cada emissão como um pagamento.

## Setup (uma vez)

O servidor roda na máquina do dono, por stdio, sem token e sem URL pública:

```bash
claude mcp add nfse-digital -- <caminho>/bin/nfse mcp
```

O painel (`nfse serve` → Integrações) mostra o comando já com o caminho certo e o mesmo passo para
Codex e Claude Desktop. Se as ferramentas `nfse-digital` não aparecerem, mande o dono para essa tela.
Nunca peça o certificado nem a senha dele: o A1 sai do `pass`.

## Ferramentas

| Ferramenta | Para quê |
|---|---|
| `account_status` | emitente, ambiente, numeração, validade do certificado, o que falta configurar |
| `list_customers` | tomadores cadastrados (filtra por nome ou documento) |
| `list_templates` | modelos de serviço: código de tributação, valor e descrição padrão |
| `preview_invoice` | espelho da nota antes de emitir: não reserva número, não grava, não transmite |
| `emit_invoice` | emite a nota (exige `idempotency_key`; produção exige `confirm_production`) |
| `list_invoices` / `show_invoice` | histórico e detalhe (chave, número, status, erro traduzido) |
| `download_danfse` | devolve o PDF do DANFSe e o XML autorizado, ambos em base64 |
| `cancel_invoice` | cancela uma nota autorizada |
| `list_schedules` | agendamentos mensais e as próximas emissões |

## Antes de tudo

Chame `account_status`. Ele diz o ambiente (**Homologação** não tem valor fiscal; **Produção**
tem), a numeração e o que falta configurar. Se `missing` não estiver vazio, resolva isso com o
dono antes de emitir.

## Antes de emitir: `preview_invoice`

Nota autorizada só se desfaz por cancelamento. **Monte o espelho e espere o dono aprovar** —
vale igual em produção e em homologação.

```
preview_invoice(customer_id, template_id, amount, competence, description)
```

Devolve ambiente (`is_production`), emitente, tomador, modelo, `service_code`, `competence`,
**`service_month`** (= competência − 1 mês, o mês do serviço), `amount`, a `description`
integral e `next_dps_number`. **Não reserva número, não grava e não transmite.**

Mostre o espelho com a descrição inteira — é o campo que mais erra e o erro só aparece no PDF
depois de autorizada. Só com o "sim" do dono, chame `emit_invoice` com os mesmos campos mais a
`idempotency_key` (e `confirm_production` em produção).

## Emitir — sempre nesta ordem

1. Ache o tomador em `list_customers` e o modelo em `list_templates`. Não invente ID.
2. Monte o resumo e **mostre ao dono**: tomador (nome e documento), modelo, valor, competência,
   descrição e ambiente. Valor e competência vazios usam o padrão do modelo e o dia de hoje —
   diga isso no resumo, não deixe implícito.
3. Só com o "sim" dele — dado sobre o espelho do `preview_invoice` — chame `emit_invoice` com uma `idempotency_key` nova e estável (por
   exemplo `tomador-AAAA-MM-valor`). Em produção, `confirm_production: true` — e só se ele
   confirmou que é produção.
4. Leia o `status` da resposta:
   - `autorizada`: mostre número da NFS-e e chave de acesso.
   - `rejeitada`: mostre `error_code` e `message` (já traduzida). Não reemita sem corrigir a causa.
   - `pendente`: a SEFIN não respondeu. **Não emita de novo.** Consulte com `show_invoice`.

### Nunca emitir duas vezes

- Tentar de novo o MESMO pedido = mesma `idempotency_key`. A resposta devolve a nota já criada.
- Pedido novo (outro mês, outro valor) = chave nova.
- Vários tomadores de uma vez: confirme a lista inteira antes e use uma chave por nota.

## Cancelar

1. `show_invoice` para conferir que a nota está `autorizada`.
2. Pergunte o motivo — `erro_emissao`, `servico_nao_prestado` ou `outros` — e uma justificativa
   de 15 a 255 caracteres, nas palavras do dono.
3. Avise que é irreversível e, com confirmação explícita, chame `cancel_invoice`
   (`confirm_production: true` se a nota for de produção).
4. Recusa por prazo vem com a orientação de pedir na prefeitura: repasse.

## DANFSe

`download_danfse` gera o PDF localmente (a API de DANFSe do ADN está suspensa desde 03/08/2026,
NT-008) e devolve **o conteúdo**, não um caminho:

| Campo | O que é |
|---|---|
| `content_base64` · `bytes` · `file_name` | o PDF do DANFSe |
| `xml_base64` · `xml_bytes` · `xml_file_name` | o XML autorizado pela SEFIN, que é o documento fiscal em si |
| `path` | só aparece quando o servidor conseguiu gravar uma cópia em disco |

Só nota autorizada ou cancelada.

⚠️ **`path` não serve ao cliente quando o servidor é remoto.** Até 04/11/2026 esta ferramenta
devolvia apenas o caminho e falhava inteira se não conseguisse gravar: contra
`nfse.bassi.engineer` a resposta era `open /home/nonroot/Downloads/…: no such file or
directory`, e não havia como o agente obter o PDF. Hoje o conteúdo é o produto e a cópia em
disco é opcional — travado no teste do `mcpserver` com o diretório inexistente.

## Erros comuns da SEFIN

| Código | O que fazer |
|---|---|
| E0116 | falta a inscrição municipal do prestador (ou o cadastro de teste do município não tem a empresa) |
| E0120 | a IM não deve ir neste ambiente — comum em homologação |
| E0160 / E0166 | opção ou regime do Simples Nacional não confere com a Receita |
| E1831 | a nota não existe no ambiente consultado — confira homologação × produção |

## O que este servidor ainda não faz

Cadastrar tomador, consultar CNPJ na Receita, ajustar o valor de um mês agendado e pular um
mês. Para isso, use a CLI (`nfse schedule …`) ou o painel.
