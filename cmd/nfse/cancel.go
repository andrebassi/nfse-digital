package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"time"

	"nfse/internal/adapters/sefin"
	"nfse/internal/app"
	"nfse/internal/domain"
)

// cancelCommand registra o evento de cancelamento de uma nota autorizada.
// Nota de produção exige --production, que confirma na guarda do app e libera
// o adapter de produção.
func cancelCommand(args []string) error {
	flags := flag.NewFlagSet("cancel", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	reason := flags.String("reason", "", "motivo: erro_emissao, servico_nao_prestado ou outros")
	description := flags.String("description", "", "justificativa, de 15 a 255 caracteres")
	production := flags.Bool("production", false, "confirma que a nota é de produção (valor fiscal)")
	showXML := flags.Bool("show-xml", false, "imprime o pedido enviado e a resposta crua da SEFIN")
	if err := flags.Parse(args); err != nil {
		return err
	}
	id, err := strconv.ParseInt(flags.Arg(0), 10, 64)
	if err != nil {
		return errors.New("uso: nfse cancel [--db arquivo] --reason <motivo> --description <texto> [--production] <id>")
	}
	ctx := context.Background()
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	canceller := &sefin.Canceller{Certificates: certificatesFor(cliAccount), AllowProduction: *production}
	service := &app.Service{Repo: repo, Now: time.Now, Location: domain.SaoPaulo()}
	report, err := service.Cancel(ctx, canceller, id, *reason, *description, "nfse-digital "+version, *production)
	if *showXML {
		fmt.Printf("--- pedido enviado ---\n%s\n--- resposta crua da SEFIN ---\n%s\n", canceller.LastSentXML, canceller.LastRaw)
	}
	var fields domain.FieldErrors
	if errors.As(err, &fields) {
		return fmt.Errorf("dados inválidos: %w", fields)
	}
	if err != nil {
		return err
	}
	if !report.Registered {
		return fmt.Errorf("a SEFIN recusou o cancelamento da nota #%d: %s", id, report.Invoice.ErrorMessage)
	}
	fmt.Printf("nota #%d (NFS-e nº %s) cancelada na SEFIN\n", id, report.Invoice.NFSeNumber)
	return nil
}
