# SDKs da API REST do nfse-digital

Clientes **Go**, **Python** e **TypeScript** para a [API REST](../README.md#73-api-rest-integração-de-sistemas): emitir, listar, consultar e cancelar NFS-e e baixar o DANFSe. Os três são **gerados** do mesmo contrato, [`internal/adapters/web/api/openapi.json`](../internal/adapters/web/api/openapi.json) (OpenAPI 3.1, validado com o CLI do [Scalar](https://github.com/scalar/scalar)), por geradores de código aberto com versão fixa:

| Linguagem | Pasta | Gerador | Nome |
|---|---|---|---|
| Go | [`go/`](go) | [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) v2.5.1 | módulo `github.com/andrebassi/nfse-digital/sdk/go`, pacote `nfse` |
| Python | [`python/`](python) | [openapi-python-client](https://github.com/openapi-generators/openapi-python-client) 0.28.2 | pacote `nfse-digital`, módulo `nfse_digital` (Python ≥ 3.10) |
| TypeScript | [`typescript/`](typescript) | [@hey-api/openapi-ts](https://heyapi.dev/) 0.99.0 | `@andrebassi/nfse-digital` (cliente `fetch`, ESM, sem dependência em runtime) |

Antes de começar: gere um **token** no painel, em **Integrações → API REST → Gerar token**. A URL base é `https://<seu-painel>/api/v1`. Toda emissão leva uma `idempotency_key` **sua** — use o id da fatura do seu sistema: repetir a mesma chave devolve a nota já criada e nunca emite outra. Em produção, mande também `confirm_production: true`.

## Go

```bash
go get github.com/andrebassi/nfse-digital/sdk/go@main
```

```go
import nfse "github.com/andrebassi/nfse-digital/sdk/go"

client, err := nfse.NewWithToken("https://nfse.bassi.engineer/api/v1", os.Getenv("NFSE_TOKEN"))
res, err := client.EmitInvoiceWithResponse(ctx, nfse.EmitRequest{
	IdempotencyKey: "fatura-2026-11-0042", CustomerId: 1, TemplateId: 1,
})
switch {
case res.JSON201 != nil: // nota nova (autorizada ou rejeitada: veja res.JSON201.Status)
case res.JSON200 != nil: // a chave já tinha nota: é a mesma, nada foi emitido
case res.JSON202 != nil: // SEFIN sem resposta: consulte com GetInvoiceByKeyWithResponse antes de tentar de novo
case res.JSON422 != nil: // dado inválido: *res.JSON422.Fields diz o campo
}
```

Cada operação tem a versão `...WithResponse`, com uma resposta tipada por status (`JSON200`, `JSON201`, `JSON404`…).

## Python

```bash
pip install "nfse-digital @ git+https://github.com/andrebassi/nfse-digital.git#subdirectory=sdk/python"
# ou: uv add "nfse-digital @ git+https://github.com/andrebassi/nfse-digital.git#subdirectory=sdk/python"
```

```python
from nfse_digital import AuthenticatedClient
from nfse_digital.api.notas import emit_invoice, get_invoice_by_key
from nfse_digital.models import EmitRequest

client = AuthenticatedClient(base_url="https://nfse.bassi.engineer/api/v1", token=os.environ["NFSE_TOKEN"])
response = emit_invoice.sync_detailed(client=client, body=EmitRequest(
    idempotency_key="fatura-2026-11-0042", customer_id=1, template_id=1))
print(response.status_code, response.parsed)   # 201 → Invoice; 422 → Error com .fields

invoice = get_invoice_by_key.sync(client=client, idempotency_key="fatura-2026-11-0042")
```

Os módulos seguem as tags do contrato: `api.notas`, `api.cadastros`, `api.conta`. Cada operação tem `sync`, `sync_detailed`, `asyncio` e `asyncio_detailed`. Limite conhecido: o gerador não tipa resposta em PDF — no `get_invoice_danf_se`, o PDF vem em `response.content`.

## TypeScript

O pacote ainda não está no npm: copie [`typescript/src`](typescript/src) para o seu projeto (ou use como dependência de workspace). Funciona em Node ≥ 18 e no navegador.

```ts
import { createClient } from "./nfse/client/index.js";
import { emitInvoice, getInvoiceByKey } from "./nfse/index.js";

const client = createClient({ baseUrl: "https://nfse.bassi.engineer/api/v1", auth: () => process.env.NFSE_TOKEN! });
const { data, error, response } = await emitInvoice({
  client,
  body: { idempotency_key: "fatura-2026-11-0042", customer_id: 1, template_id: 1 },
});
if (response?.status === 422) console.log(error?.fields);   // campo inválido
const invoice = await getInvoiceByKey({ client, query: { idempotency_key: "fatura-2026-11-0042" } });
```

O cliente não traz URL padrão de propósito: sem `baseUrl`, nenhuma nota vai para o host errado.

## Manter

| Task | O que faz |
|---|---|
| `task sdk:generate` | valida o contrato com o Scalar CLI e regera os três SDKs |
| `task sdk:check` | regera e **reprova** se o SDK versionado divergir do contrato (para o CI) |
| `task sdk:smoke` | roda o mesmo roteiro nos três SDKs contra o binário real: banco temporário, token gerado pela tela, sem SEFIN |

Mudou a API? Atualize o `openapi.json` (o teste do pacote `web` reprova se rota e contrato divergirem), rode `task sdk:generate` e versione junto.

Arquivos escritos à mão (o resto é gerado e não se edita): [`go/nfse.go`](go/nfse.go) (atalho `NewWithToken`) e o teste dele, [`go/smoke/main.go`](go/smoke/main.go), [`python_smoke.py`](python_smoke.py), [`typescript/smoke.ts`](typescript/smoke.ts), as configurações dos geradores e este README. Código gerado fica fora do gate de cobertura (os SDKs nem estão em `internal/`); a prova de que funcionam é o `sdk:smoke`.
