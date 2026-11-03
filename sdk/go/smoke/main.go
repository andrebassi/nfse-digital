// Command smoke confere o SDK Go contra um painel de verdade (scripts/sdk-smoke.sh
// sobe um num banco temporário). Lê NFSE_URL e NFSE_TOKEN, roda o roteiro
// comum aos três SDKs e sai rc=1 se alguma checagem falhar. Não emite nota:
// emitir de verdade falaria com a SEFIN.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	nfse "github.com/andrebassi/nfse-digital/sdk/go"
)

// main roda as checagens e imprime uma linha por resultado.
func main() {
	ctx := context.Background()
	url, token := os.Getenv("NFSE_URL"), os.Getenv("NFSE_TOKEN")
	client, err := nfse.NewWithToken(url, token)
	if err != nil {
		fmt.Println("🛑 go: cliente:", err)
		os.Exit(1)
	}
	failures := 0
	check := func(name string, ok bool) {
		if ok {
			fmt.Println("✅ go:", name)
			return
		}
		fmt.Println("🛑 go:", name)
		failures++
	}

	account, err := client.GetAccountWithResponse(ctx)
	check("getAccount 200 em homologação", err == nil && account.JSON200 != nil && account.JSON200.Environment != nil && *account.JSON200.Environment == nfse.Homologation)

	query := "ipe"
	customers, err := client.ListCustomersWithResponse(ctx, &nfse.ListCustomersParams{Q: &query})
	check("listCustomers acha o tomador", err == nil && customers.JSON200 != nil && len(customers.JSON200.Customers) == 1)

	limit := 5
	invoices, err := client.ListInvoicesWithResponse(ctx, &nfse.ListInvoicesParams{Limit: &limit})
	check("listInvoices 200", err == nil && invoices.StatusCode() == http.StatusOK && invoices.JSON200 != nil)

	refused, err := client.EmitInvoiceWithResponse(ctx, nfse.EmitRequest{CustomerId: 1, TemplateId: 1})
	check("emitInvoice sem chave → 422 com o campo", err == nil && refused.JSON422 != nil && refused.JSON422.Fields != nil && (*refused.JSON422.Fields)["idempotency_key"] != "")

	missing, err := client.GetInvoiceByKeyWithResponse(ctx, &nfse.GetInvoiceByKeyParams{IdempotencyKey: "nao-existe"})
	check("getInvoiceByKey desconhecida → 404", err == nil && missing.JSON404 != nil)

	intruder, _ := nfse.NewWithToken(url, "nf_errado")
	denied, err := intruder.GetAccountWithResponse(ctx)
	check("token errado → 401", err == nil && denied.JSON401 != nil)

	if failures > 0 {
		os.Exit(1)
	}
}
