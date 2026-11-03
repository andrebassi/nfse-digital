"""Confere o SDK Python contra um painel de verdade.

O scripts/sdk-smoke.sh sobe um painel num banco temporário e roda este
arquivo com NFSE_URL e NFSE_TOKEN. É o mesmo roteiro dos outros dois SDKs;
sai rc=1 se alguma checagem falhar. Não emite nota: emitir de verdade
falaria com a SEFIN.
"""

import os
import sys

from nfse_digital import AuthenticatedClient
from nfse_digital.api.cadastros import list_customers
from nfse_digital.api.conta import get_account
from nfse_digital.api.notas import emit_invoice, get_invoice_by_key, list_invoices
from nfse_digital.models import EmitRequest, Environment


def main() -> int:
    """Roda as checagens e imprime uma linha por resultado; devolve o rc."""
    url, token = os.environ["NFSE_URL"], os.environ["NFSE_TOKEN"]
    client = AuthenticatedClient(base_url=url, token=token)
    failures = 0

    def check(name: str, ok: bool) -> None:
        """Imprime o resultado de uma checagem e conta as falhas."""
        nonlocal failures
        print(("✅" if ok else "🛑") + " python: " + name)
        failures += 0 if ok else 1

    account = get_account.sync_detailed(client=client)
    check("getAccount 200 em homologação", account.status_code == 200 and account.parsed.environment == Environment.HOMOLOGATION)

    customers = list_customers.sync(client=client, q="ipe")
    check("listCustomers acha o tomador", customers is not None and len(customers.customers) == 1)

    invoices = list_invoices.sync_detailed(client=client, limit=5)
    check("listInvoices 200", invoices.status_code == 200)

    refused = emit_invoice.sync_detailed(client=client, body=EmitRequest(idempotency_key="", customer_id=1, template_id=1))
    fields = refused.parsed.fields.to_dict() if refused.parsed is not None and refused.parsed.fields else {}
    check("emitInvoice sem chave → 422 com o campo", refused.status_code == 422 and "idempotency_key" in fields)

    missing = get_invoice_by_key.sync_detailed(client=client, idempotency_key="nao-existe")
    check("getInvoiceByKey desconhecida → 404", missing.status_code == 404)

    intruder = AuthenticatedClient(base_url=url, token="nf_errado")
    check("token errado → 401", get_account.sync_detailed(client=intruder).status_code == 401)

    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
