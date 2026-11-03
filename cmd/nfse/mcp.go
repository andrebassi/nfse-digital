package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nfse/internal/adapters/mcpserver"
	"nfse/internal/adapters/sefin"
	"nfse/internal/adapters/web"
	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// mcpCommand sobe o servidor MCP por stdio, com o banco real e os adapters
// reais da SEFIN. O certificado só é aberto quando uma ferramenta precisa
// dele (emitir, cancelar, buscar XML, account_status).
func mcpCommand(args []string) error {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	outputDir := flags.String("output-dir", "", "onde o download_danfse grava os PDFs (padrão: diretório atual)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	if *outputDir == "" {
		if *outputDir, err = os.Getwd(); err != nil {
			return err
		}
	}
	return mcpserver.New(mcpDeps(repo, *outputDir, cliAccount)).Run(context.Background(), &mcp.StdioTransport{})
}

// mcpDeps monta o servidor MCP com o banco e os adapters reais; serve ao
// stdio (`nfse mcp`) e ao HTTP do painel (`/mcp`, com token).
func mcpDeps(repo ports.Repository, outputDir string, accountID int64) mcpserver.Deps {
	store := certificatesFor(accountID)
	// AllowProduction ligado: no MCP a confirmação de produção é o argumento
	// confirm_production, conferido pela guarda do app antes de cada chamada.
	emitter := &sefin.Emitter{Certificates: store, AppVersion: "nfse-digital " + version, AllowProduction: true}
	service := &app.Service{Repo: repo, Emitter: emitter, Now: time.Now, Location: domain.SaoPaulo()}
	// Emissão pelo agente também dispara o envio automático do tomador.
	if err := withEmail(service, accountID); err != nil {
		log.Printf("MCP sem envio por e-mail: %v", err)
	}
	return mcpserver.Deps{
		Service:    service,
		Canceller:  &sefin.Canceller{Certificates: store, AllowProduction: true},
		AppVersion: version,
		OutputDir:  outputDir,
		FetchXML: func(ctx context.Context, invoice domain.Invoice) ([]byte, error) {
			certificate, err := store.Load(ctx)
			if err != nil {
				return nil, err
			}
			consulter := sefin.Consulter{Reader: sefin.NewReader(certificate.TLS, nil), SefinURL: sefin.BaseURL(invoice.Environment)}
			return consulter.NFSeXML(ctx, invoice.AccessKey)
		},
		Certificate: func(ctx context.Context) (domain.CertificateInfo, error) {
			certificate, err := store.Load(ctx)
			return certificate.Info, err
		},
	}
}

// integration monta os dados da tela de Integrações: o caminho absoluto deste
// binário (os comandos saem prontos para colar), o catálogo e a skill.
func integration() web.Integration {
	binary, err := os.Executable()
	if err != nil {
		binary = "nfse"
	}
	tools := make([]web.IntegrationTool, 0, len(mcpserver.Tools))
	for _, tool := range mcpserver.Tools {
		tools = append(tools, web.IntegrationTool{Name: tool.Name, Description: tool.Description, Writes: tool.Writes})
	}
	return web.Integration{Binary: binary, Tools: tools, Skill: mcpserver.Skill}
}
