package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"nfse/internal/domain"
)

// settingsCommand troca o ambiente do emitente. Ir para produção pede o CNPJ
// digitado de novo no terminal (story 4.1): a flag sozinha num script ou no
// histórico do shell não basta para ligar nota com valor fiscal.
func settingsCommand(args []string) error {
	return changeEnvironment(args, os.Stdin, os.Stdout)
}

// changeEnvironment é o corpo do comando, com entrada e saída injetadas.
func changeEnvironment(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("settings", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	environment := flags.String("environment", "", "homologation ou production")
	if err := flags.Parse(args); err != nil {
		return err
	}
	target := domain.Environment(*environment)
	if target != domain.EnvHomologation && target != domain.EnvProduction {
		return errors.New("uso: nfse settings [--db arquivo] --environment <homologation|production>")
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	ctx := context.Background()
	issuer, err := repo.Issuer(ctx)
	if err != nil {
		return fmt.Errorf("configure o emitente antes: %w", err)
	}
	if target == domain.EnvProduction {
		fmt.Fprintf(output, "Produção emite nota com valor fiscal. Digite o CNPJ do emitente (%s) para confirmar: ", issuer.LegalName)
		line, _ := bufio.NewReader(input).ReadString('\n')
		if !domain.ConfirmsCNPJ(issuer.CNPJ, line) {
			return errors.New("CNPJ não confere: o ambiente continua " + issuer.Environment.Label())
		}
	}
	issuer.Environment = target
	if err := repo.SaveIssuer(ctx, issuer); err != nil {
		return err
	}
	fmt.Fprintf(output, "ambiente do emitente: %s\n", target.Label())
	return nil
}
