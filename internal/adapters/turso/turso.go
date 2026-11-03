// Package turso abre o banco do emissor no Turso (libSQL remoto) em vez de um
// arquivo SQLite local — para o deploy no Fly rodar sem volume. Só cuida de
// COMO a conexão nasce: schema, migrações e consultas continuam no adapter
// sqlite (OpenWithDB), o mesmo desenho do tursostore do duxfleet.
//
// O driver é o tursogo-serverless, em Go puro, que fala HTTP (Hrana) e mantém
// o build com CGO_ENABLED=0. O go-libsql (embedded replica) ficou de fora:
// exige CGO e um arquivo local, justamente o que o deploy sem volume dispensa.
package turso

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	tursodriver "turso.tech/database/tursogo-serverless"

	"nfse/internal/adapters/sqlite"
)

// connectTimeout é o prazo da primeira consulta: com URL ou token errados o
// serve tem de morrer com a mensagem certa, não ficar pendurado no deploy.
const connectTimeout = 15 * time.Second

// maxIdle é quanto uma conexão pode ficar parada no pool: abaixo do prazo em
// que o Turso descarta o stream dela (cerca de 10 s sem uso).
const maxIdle = 5 * time.Second

// ErrNoToken recusa abrir o Turso sem token: o servidor responderia 401 só
// na primeira consulta, com uma mensagem que não aponta o secret faltando.
var ErrNoToken = errors.New("NFSE_DB aponta para o Turso, mas TURSO_AUTH_TOKEN está vazio")

// IsRemote diz se o NFSE_DB é uma URL do Turso (libsql://, https://,
// http://) em vez de um caminho de arquivo.
func IsRemote(dsn string) bool {
	for _, scheme := range []string{"libsql://", "https://", "http://"} {
		if strings.HasPrefix(dsn, scheme) {
			return true
		}
	}
	return false
}

// Open conecta no Turso e devolve o mesmo repositório do SQLite local. O
// token vai separado da URL (nunca concatenado nela), para não aparecer em
// log que imprima a URL; ele vem do secret TURSO_AUTH_TOKEN.
func Open(url, authToken string) (*sqlite.Repository, error) {
	if authToken == "" {
		return nil, ErrNoToken
	}
	db := sql.OpenDB(tursodriver.NewConnector(url, authToken))
	// Cada conexão do pool carrega um stream do Turso, que o servidor
	// descarta depois de alguns segundos parado. Reaproveitar uma conexão
	// velha (a máquina do Fly acordando da suspensão) dava "stream not
	// found" (medido em 30/10/2026, 06:25:57): conexão ociosa sai do pool
	// antes disso e a próxima consulta abre uma nova.
	db.SetConnMaxIdleTime(maxIdle)
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("conectar no Turso: %w", err)
	}
	return sqlite.OpenWithDB(db)
}
