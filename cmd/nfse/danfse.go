package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"nfse/internal/adapters/danfse"
	"nfse/internal/adapters/nfsexml"
	"nfse/internal/adapters/sefin"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// danfseCommand gera o PDF do DANFSe de nota autorizada ou cancelada. Usa o XML
// guardado; sem ele, busca na SEFIN pela chave (só leitura) e guarda.
func danfseCommand(args []string) error {
	flags := flag.NewFlagSet("danfse", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	output := flags.String("o", "", "arquivo PDF (padrão: nfse-<número>-<tomador>.pdf)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	id, err := strconv.ParseInt(flags.Arg(0), 10, 64)
	if err != nil {
		return errors.New("uso: nfse danfse [--db arquivo] [-o arquivo.pdf] <id>")
	}
	ctx := context.Background()
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	invoice, err := repo.Invoice(ctx, id)
	if err != nil {
		return err
	}
	pdf, name, err := renderDANFSe(ctx, repo, invoice)
	if err != nil {
		return err
	}
	if *output == "" {
		*output = name
	}
	if err := os.WriteFile(*output, pdf, 0o600); err != nil {
		return err
	}
	fmt.Printf("DANFSe da NFS-e nº %s gravado em %s (%d bytes)\n", invoice.NFSeNumber, *output, len(pdf))
	return nil
}

// renderDANFSe monta o PDF e o nome padrão do arquivo. Sem XML guardado, busca
// na SEFIN pela chave (abre o certificado) e guarda para a próxima vez.
func renderDANFSe(ctx context.Context, repo ports.Repository, invoice domain.Invoice) ([]byte, string, error) {
	// Só nota que a SEFIN aceitou tem DANFSe; a cancelada continua imprimível
	// porque o tomador pode pedir a cópia do documento que foi anulado.
	if invoice.AccessKey == "" || (invoice.Status != domain.StatusAuthorized && invoice.Status != domain.StatusCancelled) {
		return nil, "", fmt.Errorf("a nota está %q: só nota autorizada ou cancelada tem DANFSe", invoice.Status)
	}
	if len(invoice.XML) == 0 {
		certificate, err := certificatesFor(cliAccount).Load(ctx)
		if err != nil {
			return nil, "", err
		}
		consulter := sefin.Consulter{Reader: sefin.NewReader(certificate.TLS, nil), SefinURL: sefin.BaseURL(invoice.Environment)}
		if invoice.XML, err = consulter.NFSeXML(ctx, invoice.AccessKey); err != nil {
			return nil, "", err
		}
		if err := repo.SaveInvoice(ctx, &invoice); err != nil {
			return nil, "", err
		}
	}
	data, err := nfsexml.ParseDANFSe(invoice.XML)
	if err != nil {
		return nil, "", err
	}
	pdf, err := danfse.Render(data)
	return pdf, danfse.FileName(data), err
}
