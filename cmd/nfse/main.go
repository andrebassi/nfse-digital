// Comando nfse: emissor pessoal de NFS-e. Este arquivo só monta as peças
// (wiring) e fica fora do gate de cobertura.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nfse/internal/adapters/mcpserver"
	"nfse/internal/adapters/passcert"
	"nfse/internal/adapters/sefin"
	"nfse/internal/adapters/sqlite"
	"nfse/internal/adapters/web"
	"nfse/internal/app"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// version é trocada no build por -ldflags "-X main.version=...".
var version = "0.1.0-prototype"

// main despacha o subcomando: version, serve, cert check, emit, list ou show.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: nfse <version|serve|cert check|emit|list|show|import|fetch|sync|danfse|cancel|schedule|settings|mcp|skill> [opções]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "skill":
		// Imprime a skill do agente, para instalar com um redirecionamento.
		fmt.Print(mcpserver.Skill)
		return
	case "version":
		fmt.Println("nfse", version)
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "cert":
		if len(os.Args) < 3 || os.Args[2] != "check" {
			fmt.Fprintln(os.Stderr, "uso: nfse cert check")
			os.Exit(2)
		}
		if err := certCheck(); err != nil {
			fmt.Fprintln(os.Stderr, "🛑", err)
			os.Exit(1)
		}
	case "emit", "list", "show", "import", "fetch", "sync", "danfse", "cancel", "schedule", "settings", "mcp":
		commands := map[string]func([]string) error{"emit": emitCommand, "list": listCommand, "show": showCommand, "import": importCommand, "fetch": fetchCommand, "sync": syncCommand, "danfse": danfseCommand, "cancel": cancelCommand, "schedule": scheduleCommand, "settings": settingsCommand, "mcp": mcpCommand}
		if err := commands[os.Args[1]](os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "🛑", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "comando desconhecido: %s\n", os.Args[1])
		os.Exit(2)
	}
}

// certCheck lê o A1 do pass (prefixo em NFSE_PASS_PREFIX, padrão bassi/nfse)
// e mostra titular, CNPJ, AC e validade, com aviso a menos de 30 dias.
func certCheck() error {
	prefix := os.Getenv("NFSE_PASS_PREFIX")
	if prefix == "" {
		prefix = passcert.DefaultPrefix
	}
	certificate, err := passcert.Store{Prefix: prefix, Now: time.Now}.Load(context.Background())
	if err != nil {
		return err
	}
	info := certificate.Info
	location := domain.SaoPaulo()
	fmt.Printf("titular:  %s\nCNPJ:     %s\nemissor:  %s\nválido:   %s até %s (%d dias)\ncadeia:   %d certificado(s) no TLS\n",
		info.HolderName, domain.FormatDocument(info.CNPJ), info.IssuerName,
		info.NotBefore.In(location).Format("02/01/2006"), info.NotAfter.In(location).Format("02/01/2006 15:04"),
		info.DaysLeft(time.Now()), len(certificate.TLS.Certificate))
	if info.ExpiresSoon(time.Now()) {
		fmt.Println("⚠️  vence em menos de 30 dias: providencie o novo A1")
	}
	return nil
}

// serve sobe o painel (127.0.0.1, ou o host público no Fly) com o SQLite e o emissor real
// da SEFIN. Escuta só em 127.0.0.1: o login protege contra outro usuário da
// máquina, não contra a rede.
func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := flags.Int("port", 8735, "porta em 127.0.0.1")
	dbPath := flags.String("db", "", "arquivo SQLite (padrão: NFSE_DB ou ~/.local/share/nfse-digital/nfse.db)")
	runSchedules := flags.Bool("run-schedules", false, "roda o agendador a cada minuto com o emissor REAL da SEFIN")
	if err := flags.Parse(args); err != nil {
		return err
	}
	path := *dbPath
	if path == "" {
		var err error
		if path, err = sqlite.DefaultPath(); err != nil {
			return err
		}
	}
	database, err := openStore(path)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := configureVault(database); err != nil {
		return err
	}
	// O agendador emite de verdade, então só liga com pedido explícito.
	if *runSchedules {
		go scheduleLoop(database)
	}
	// Conta zero: o certificado é sempre o da conta da sessão (context). A
	// guarda de produção é a caixa de confirmação do formulário (4.1).
	emitter := &sefin.Emitter{Certificates: certificatesFor(0), AppVersion: "nfse-digital " + version, AllowProduction: true}
	// NFSE_SIGNUP_EMAILS (secret no Fly) fecha o cadastro aos e-mails da
	// lista; vazia, o cadastro fica aberto, como na máquina do dono.
	service := &app.Service{Repo: database, Emitter: emitter, Now: time.Now, Location: domain.SaoPaulo(),
		SignUpEmails: app.ParseSignUpEmails(os.Getenv("NFSE_SIGNUP_EMAILS"))}
	if err := withEmail(service, 0); err != nil {
		return err
	}
	if service.SignUpEmails == nil && os.Getenv("NFSE_PUBLIC_HOST") != "" {
		return errors.New("NFSE_PUBLIC_HOST exige NFSE_SIGNUP_EMAILS: painel público não abre cadastro a qualquer um")
	}
	server, err := web.New(service, *port)
	if err != nil {
		return err
	}
	server.UseIntegration(integration())
	server.UseDANFSe(func(ctx context.Context, invoice domain.Invoice) ([]byte, string, error) {
		return renderDANFSe(ctx, database, invoice)
	})
	server.UsePortal(portalOps(service))
	// Mesmo certificado por conta da emissão (conta zero = a da sessão). A guarda
	// de produção é a caixa de confirmação do formulário de cancelamento.
	server.UseCanceller(&sefin.Canceller{Certificates: certificatesFor(0), AllowProduction: true}, "nfse-digital "+version)
	// MCP por HTTP em /mcp, com token: o mesmo servidor do `nfse mcp`. Com
	// sessão (o padrão do protocolo), para o servidor guardar o initialize e
	// saber o nome do agente que aparece em "Testar conexão". Um servidor MCP
	// por conta, preso aos dados dela: a conta vem do token, conferido pelo
	// portão do painel antes de chegar aqui.
	home, _ := os.UserHomeDir()
	servers := map[int64]*mcp.Server{}
	var serversMu sync.Mutex
	server.UseMCP(mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		accountID := ports.AccountID(r.Context())
		serversMu.Lock()
		defer serversMu.Unlock()
		if servers[accountID] == nil {
			servers[accountID] = mcpserver.New(mcpDeps(database.For(accountID), filepath.Join(home, "Downloads"), accountID))
		}
		return servers[accountID]
	}, nil))
	store := certificatesFor(0)
	server.UseCertificate(web.CertificateOps{Replace: store.Replace, Storage: certificateStorage()})
	publicHost := os.Getenv("NFSE_PUBLIC_HOST")
	address, err := listenAddress(os.Getenv("NFSE_LISTEN"), *port, publicHost)
	if err != nil {
		return err
	}
	if publicHost != "" {
		if err := server.UsePublicHost(publicHost); err != nil {
			return err
		}
	}
	log.Printf("nfse-digital %s: escutando em %s (público: %q) · banco %s · cofre de certificado: %t", version, address, publicHost, path, vault != nil)
	httpServer := &http.Server{
		Addr:              address,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// certificateStorage é a frase da tela sobre onde o A1 fica: o cofre cifrado
// no banco (Fly) ou o pass da máquina.
func certificateStorage() string {
	if vault != nil {
		return "Depois, o arquivo e a senha são gravados juntos, cifrados (AES-256-GCM) no banco; a chave fica só no servidor."
	}
	return "Depois, o arquivo e a senha são gravados cifrados no pass desta máquina."
}

// listenAddress decide onde o painel escuta. Sem NFSE_LISTEN, 127.0.0.1 na
// porta da flag (a máquina do dono). Endereço fora do loopback (0.0.0.0:8080
// no Fly) só com NFSE_PUBLIC_HOST: sem ele o portão de Host aceitaria só
// 127.0.0.1, e um esquecimento de variável não pode abrir o painel na rede.
func listenAddress(listen string, port int, publicHost string) (string, error) {
	if listen == "" {
		return fmt.Sprintf("127.0.0.1:%d", port), nil
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("NFSE_LISTEN inválido: %w", err)
	}
	ip := net.ParseIP(host)
	if publicHost == "" && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("NFSE_LISTEN=%s fora do loopback exige NFSE_PUBLIC_HOST", listen)
	}
	return listen, nil
}

// scheduleLoop roda o agendador de CADA conta no começo de cada minuto e
// registra no log o que cada rodada fez. O agendador de uma conta é guardado
// entre as rodadas, para o intervalo de reenvio valer. Uma rodada que falha
// não para as seguintes nem as das outras contas.
func scheduleLoop(database *sqlite.Repository) {
	log.Printf("agendador ligado: rodada a cada minuto, emissor real da SEFIN, todas as contas")
	schedulers := map[int64]*app.Scheduler{}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for now := range ticker.C {
		accounts, err := database.Accounts(context.Background())
		if err != nil {
			log.Printf("agendador: listar contas: %v", err)
			continue
		}
		for _, account := range accounts {
			if schedulers[account.ID] == nil {
				schedulers[account.ID] = newScheduler(database.For(account.ID), account.ID)
			}
			outcomes, err := schedulers[account.ID].Run(context.Background(), now, false)
			if err != nil {
				log.Printf("agendador: conta #%d: %v", account.ID, err)
			}
			for _, outcome := range outcomes {
				log.Printf("agendador: conta #%d · #%d %s %s %v", account.ID, outcome.ScheduleID, outcome.Period, outcome.Action, outcome.Err)
			}
		}
	}
}
