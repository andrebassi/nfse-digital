// Confere o SDK TypeScript contra um painel de verdade. O scripts/sdk-smoke.sh
// sobe um painel num banco temporário, compila este arquivo com o SDK e roda
// com NFSE_URL e NFSE_TOKEN. Mesmo roteiro dos SDKs Go e Python; sai rc=1 se
// alguma checagem falhar. Não emite nota: emitir de verdade falaria com a SEFIN.
//
// Arquivo escrito à mão dentro do SDK TypeScript, que o dono pediu sabendo da
// regra que barra código novo em TS: só existe para exercitar o código gerado.
import { createClient } from "./src/client/index.js";
import { emitInvoice, getAccount, getInvoiceByKey, listCustomers, listInvoices } from "./src/index.js";

const baseUrl = process.env.NFSE_URL ?? "";
const token = process.env.NFSE_TOKEN ?? "";
// auth devolve o token para o esquema bearer do contrato (o SDK monta o cabeçalho).
const client = createClient({ baseUrl, auth: () => token });
const intruder = createClient({ baseUrl, auth: () => "nf_errado" });
let failures = 0;

// check imprime o resultado de uma checagem e conta as falhas.
function check(name: string, ok: boolean): void {
  console.log(`${ok ? "✅" : "🛑"} typescript: ${name}`);
  if (!ok) failures++;
}

const account = await getAccount({ client });
check("getAccount 200 em homologação", account.response?.status === 200 && account.data?.environment === "homologation");

const customers = await listCustomers({ client, query: { q: "ipe" } });
check("listCustomers acha o tomador", customers.data?.customers.length === 1);

const invoices = await listInvoices({ client, query: { limit: 5 } });
check("listInvoices 200", invoices.response?.status === 200);

const refused = await emitInvoice({ client, body: { idempotency_key: "", customer_id: 1, template_id: 1 } });
check("emitInvoice sem chave → 422 com o campo", refused.response?.status === 422 && Boolean(refused.error?.fields?.idempotency_key));

const missing = await getInvoiceByKey({ client, query: { idempotency_key: "nao-existe" } });
check("getInvoiceByKey desconhecida → 404", missing.response?.status === 404);

const denied = await getAccount({ client: intruder });
check("token errado → 401", denied.response?.status === 401);

process.exit(failures > 0 ? 1 : 0);
