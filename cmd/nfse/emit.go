package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"nfse/internal/adapters/nfsexml"
	"nfse/internal/adapters/sefin"
	"nfse/internal/adapters/web"
	"nfse/internal/app"
	"nfse/internal/domain"
)

// emitCommand emite pela SEFIN do ambiente configurado no emitente (só
// homologação até a story 4.1). Tomador pelo CPF/CNPJ, modelo pelo nome.
func emitCommand(args []string) error {
	flags := flag.NewFlagSet("emit", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	customerDoc := flags.String("customer", "", "CPF/CNPJ do tomador")
	templateName := flags.String("template", "", "nome do modelo de serviço")
	amount := flags.String("amount", "", "valor, como 19000.00 (padrão: o do modelo)")
	competence := flags.String("competence", "", "data de competência AAAA-MM-DD (padrão: hoje)")
	description := flags.String("description", "", "descrição (padrão: a do modelo, com {mes}/{ano} resolvidos)")
	showXML := flags.Bool("show-xml", false, "imprime o XML enviado e a resposta crua da SEFIN")
	production := flags.Bool("production", false, "confirma a emissão em produção (nota com valor fiscal)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	ctx := context.Background()
	location := domain.SaoPaulo()
	now := time.Now().In(location)

	document, err := domain.ParseDocument(*customerDoc)
	if err != nil {
		return fmt.Errorf("--customer: %w", err)
	}
	customers, _ := repo.Customers(ctx, document.Digits())
	if len(customers) == 0 {
		return fmt.Errorf("tomador %s não cadastrado", document)
	}
	templates, _ := repo.Templates(ctx, *templateName)
	var template *domain.Template
	for i := range templates {
		if templates[i].Name == *templateName {
			template = &templates[i]
		}
	}
	if template == nil {
		return fmt.Errorf("modelo %q não cadastrado", *templateName)
	}
	if *competence == "" {
		*competence = now.Format("2006-01-02")
	}
	if *amount == "" {
		*amount = template.DefaultAmount.InputValue()
	}
	if *description == "" {
		competenceDate, _ := time.ParseInLocation("2006-01-02", *competence, location)
		*description = domain.RenderDescription(template.Description, competenceDate)
	}

	emitter := &sefin.Emitter{Certificates: certificatesFor(cliAccount), AppVersion: "nfse-digital " + version, AllowProduction: *production}
	service := &app.Service{Repo: repo, Emitter: emitter, Now: time.Now, Location: location}
	invoice, err := service.Emit(ctx, app.EmitInput{
		CustomerID: customers[0].ID, TemplateID: template.ID, Amount: *amount, Competence: *competence, Description: *description,
		ConfirmProduction: *production,
	})
	if *showXML {
		fmt.Printf("--- XML enviado ---\n%s\n--- resposta crua da SEFIN ---\n%s\n", emitter.LastSentXML, emitter.LastRaw)
	}
	var fields domain.FieldErrors
	if errors.As(err, &fields) {
		return fmt.Errorf("dados inválidos: %w", fields)
	}
	if invoice.ID > 0 {
		printInvoice(invoice, customers[0])
	}
	return err
}

// printInvoice mostra a nota no terminal, com o retorno da SEFIN traduzido.
func printInvoice(invoice domain.Invoice, customer domain.Customer) {
	fmt.Printf("nota #%d · %s · %s · DPS %s/%d\n", invoice.ID, invoice.Status.Label(), invoice.Environment.Label(), invoice.Series, invoice.DPSNumber)
	fmt.Printf("tomador:  %s (%s)\nvalor:    %s · competência %s\n", customer.Name, domain.FormatDocument(customer.Document), invoice.Amount.FormatBRL(), invoice.Competence.Format("02/01/2006"))
	if invoice.AccessKey != "" {
		fmt.Printf("chave:    %s · NFS-e nº %s\n", invoice.AccessKey, invoice.NFSeNumber)
	}
	if invoice.ErrorCode != "" || invoice.ErrorMessage != "" {
		fmt.Printf("SEFIN:    %s\n          %s\n", invoice.ErrorMessage, domain.TranslateSefinError(invoice.ErrorCode, ""))
	}
}

// importCommand importa o XML de uma NFS-e (ou DPS) para o banco: emitente,
// tomador, modelo e a nota.
func importCommand(args []string) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	templateName := flags.String("template-name", "", "nome do modelo (padrão: Importado — tomador)")
	overwrite := flags.Bool("overwrite-issuer", false, "sobrescreve o emitente com os dados do XML")
	portal := flags.Bool("portal", false, "consulta (só leitura) as notas emitidas no ambiente nacional, em vez de um arquivo")
	production := flags.Bool("production", false, "com --portal: lê da produção em vez da homologação")
	apply := flags.String("apply", "", "com --portal: grupos a importar, separados por vírgula, ou all (vazio só lista)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *portal {
		return importPortal(*dbPath, *production, *apply)
	}
	if flags.NArg() != 1 {
		return errors.New("uso: nfse import [--db arquivo] [--template-name nome] [--overwrite-issuer] <nota.xml>")
	}
	data, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	imported, err := nfsexml.Parse(data)
	if err != nil {
		return err
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	service := &app.Service{Repo: repo, Now: time.Now, Location: domain.SaoPaulo()}
	report, err := service.Import(context.Background(), imported, *templateName, *overwrite)
	if err != nil {
		return err
	}
	fmt.Printf("emitente: %s\ntomador:  %s\nmodelo:   %s\nnota:     %s\n", report.Issuer, report.Customer, report.Template, report.Invoice)
	return nil
}

// importPortal varre a distribuição do ADN com o A1 e lista os serviços
// recorrentes; com --apply, grava os grupos escolhidos.
func importPortal(dbPath string, production bool, apply string) error {
	ctx := context.Background()
	certificate, err := certificatesFor(cliAccount).Load(ctx)
	if err != nil {
		return err
	}
	environment := domain.EnvHomologation
	if production {
		environment = domain.EnvProduction
	}
	repo, err := openDatabase(dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	service := &app.Service{Repo: repo, Now: time.Now, Location: domain.SaoPaulo()}
	distributor := sefin.Distributor{Reader: sefin.NewReader(certificate.TLS, nil), BaseURL: sefin.ADNBaseURL(environment)}
	scan, err := service.ScanPortal(ctx, distributor, app.PortalParsers{NFSe: nfsexml.Parse, Event: nfsexml.ParseEvent}, certificate.Info.CNPJ)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d notas emitidas (%d canceladas), %d em que o CNPJ é tomador, último NSU %d\n",
		environment.Label(), scan.Emitted, scan.Cancelled, scan.AsTaker, scan.LastNSU)
	for _, group := range scan.Groups {
		frequency := map[bool]string{true: "todo mês", false: "esporádico"}[group.Monthly]
		known := map[bool]string{true: " · já cadastrado", false: ""}[group.Known]
		fmt.Printf("  %-28s %2d nota(s) · %-10s · última %s %s%s\n    %s\n", group.Key, group.Count, frequency,
			group.LastNote.Invoice.Amount.FormatBRL(), group.LastNote.Invoice.IssuedAt.Format("02/01/2006"), known, group.Customer.Name)
	}
	if apply == "" {
		fmt.Println("nada gravado. Para importar: nfse import --portal --apply all (ou os grupos separados por vírgula)")
		return nil
	}
	imported, err := service.ApplyPortal(ctx, scan, strings.Split(apply, ","))
	fmt.Printf("notas importadas: %d\n", imported)
	return err
}

// syncCommand alinha as notas do banco com a SEFIN (só leitura): resolve as
// pendentes pela DPS e detecta cancelamentos feitos fora daqui.
func syncCommand(args []string) error {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	production := flags.Bool("production", false, "sincroniza as notas de produção (só leitura)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	environment := domain.EnvHomologation
	if *production {
		environment = domain.EnvProduction
	}
	certificate, err := certificatesFor(cliAccount).Load(ctx)
	if err != nil {
		return err
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	service := &app.Service{Repo: repo, Now: time.Now, Location: domain.SaoPaulo()}
	consulter := sefin.Consulter{Reader: sefin.NewReader(certificate.TLS, nil), SefinURL: sefin.BaseURL(environment), ADNURL: sefin.ADNBaseURL(environment)}
	report, err := service.Sync(ctx, consulter, app.PortalParsers{NFSe: nfsexml.Parse, Event: nfsexml.ParseEvent}, environment)
	fmt.Printf("%s: %d consultadas · %d autorizadas agora · %d canceladas agora · %d sem mudança · %d com falha\n",
		environment.Label(), report.Checked, report.Authorized, report.Cancelled, report.Unchanged, report.Failed)
	return err
}

// fetchCommand é o diagnóstico de leitura: faz um GET com o A1 numa rota da
// SEFIN ou do ADN e mostra status, tipo e o começo do corpo. Nunca envia nada.
func fetchCommand(args []string) error {
	flags := flag.NewFlagSet("fetch", flag.ContinueOnError)
	host := flags.String("host", "sefin", "sefin ou adn")
	production := flags.Bool("production", false, "lê da produção (só leitura) em vez da homologação")
	limit := flags.Int("limit", 600, "caracteres do corpo a mostrar")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("uso: nfse fetch [--host sefin|adn] [--production] <caminho, ex.: /nfse/CHAVE>")
	}
	environment := domain.EnvHomologation
	if *production {
		environment = domain.EnvProduction
	}
	base := sefin.BaseURL(environment)
	if *host == "adn" {
		base = sefin.ADNBaseURL(environment)
	}
	certificate, err := certificatesFor(cliAccount).Load(context.Background())
	if err != nil {
		return err
	}
	result, err := sefin.NewReader(certificate.TLS, nil).Get(context.Background(), base+flags.Arg(0))
	if err != nil {
		return err
	}
	body := string(result.Body)
	if len(body) > *limit {
		body = body[:*limit] + fmt.Sprintf("… (%d bytes)", len(result.Body))
	}
	fmt.Printf("GET %s%s\nHTTP %d · %s%s\n%s\n", base, flags.Arg(0), result.StatusCode, result.ContentType, map[bool]string{true: " · Location: " + result.Location}[result.Location != ""], body)
	return nil
}

// listCommand lista as notas do banco, da mais recente para a mais antiga.
func listCommand(args []string) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	if err := flags.Parse(args); err != nil {
		return err
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	invoices, err := repo.Invoices(context.Background())
	if err != nil {
		return err
	}
	for _, invoice := range invoices {
		customer, _ := repo.Customer(context.Background(), invoice.CustomerID)
		fmt.Printf("#%-4d %s  %-11s %-12s %16s  %s\n", invoice.ID, invoice.IssuedAt.Format("02/01/2006"), invoice.Status.Label(),
			invoice.Environment.Label(), invoice.Amount.FormatBRL(), customer.Name)
	}
	return nil
}

// showCommand mostra uma nota pelo número interno.
func showCommand(args []string) error {
	flags := flag.NewFlagSet("show", flag.ContinueOnError)
	dbPath := flags.String("db", "", "arquivo SQLite")
	if err := flags.Parse(args); err != nil {
		return err
	}
	id, err := strconv.ParseInt(flags.Arg(0), 10, 64)
	if err != nil {
		return errors.New("uso: nfse show [--db arquivo] <id>")
	}
	repo, err := openDatabase(*dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	invoice, err := repo.Invoice(context.Background(), id)
	if err != nil {
		return err
	}
	customer, _ := repo.Customer(context.Background(), invoice.CustomerID)
	printInvoice(invoice, customer)
	return nil
}

// parsers são os leitores de XML da varredura e do sync.
var parsers = app.PortalParsers{NFSe: nfsexml.Parse, Event: nfsexml.ParseEvent}

// portalOps monta as leituras da base nacional para o painel. O certificado é
// aberto a cada chamada (aprovação no celular) e não fica no processo.
func portalOps(service *app.Service) web.PortalOps {
	// Conta zero: só no painel, onde a conta vem da sessão (context).
	store := certificatesFor(0)
	return web.PortalOps{
		Scan: func(ctx context.Context, environment domain.Environment) (app.PortalScan, error) {
			certificate, err := store.Load(ctx)
			if err != nil {
				return app.PortalScan{}, err
			}
			distributor := sefin.Distributor{Reader: sefin.NewReader(certificate.TLS, nil), BaseURL: sefin.ADNBaseURL(environment)}
			return service.ScanPortal(ctx, distributor, parsers, certificate.Info.CNPJ)
		},
		Sync: func(ctx context.Context, environment domain.Environment) (app.SyncReport, error) {
			certificate, err := store.Load(ctx)
			if err != nil {
				return app.SyncReport{}, err
			}
			consulter := sefin.Consulter{Reader: sefin.NewReader(certificate.TLS, nil), SefinURL: sefin.BaseURL(environment), ADNURL: sefin.ADNBaseURL(environment)}
			return service.Sync(ctx, consulter, parsers, environment)
		},
	}
}
