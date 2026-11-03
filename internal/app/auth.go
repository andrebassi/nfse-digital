package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

var (
	// ErrAccountExists recusa criar conta com um e-mail que já tem conta.
	ErrAccountExists = errors.New("já existe uma conta com este e-mail: entre com ele")
	// ErrInvalidCredentials não diz se errou o e-mail ou a senha, para não
	// confirmar a quem tenta adivinhar qual e-mail está cadastrado.
	ErrInvalidCredentials = errors.New("e-mail ou senha incorretos")
	// ErrLocked é o bloqueio depois de muitas senhas erradas seguidas.
	ErrLocked = errors.New("muitas tentativas erradas: aguarde 5 minutos")
	// ErrDeleteNotConfirmed recusa excluir a conta sem o e-mail dela digitado.
	ErrDeleteNotConfirmed = errors.New("para excluir, digite o e-mail desta conta exatamente como ele aparece")
	// ErrSignUpClosed recusa cadastro de e-mail fora da lista do servidor.
	ErrSignUpClosed = errors.New("cadastro fechado: este e-mail não está autorizado neste servidor")
)

// Limites do bloqueio de login: 5 erros seguidos travam por 5 minutos. É
// pouco para atrapalhar o dono e muito para quem tenta senha no braço.
// maxTrackedClients limita a memória do contador: acima disso, quem já
// destravou sai primeiro, e um ataque de muitos IPs não enche o processo.
const (
	maxFailedAttempts = 5
	lockDuration      = 5 * time.Minute
	maxTrackedClients = 10000
)

// ParseSignUpEmails lê a lista de e-mails autorizados (separados por vírgula,
// como no secret NFSE_SIGNUP_EMAILS). Texto vazio devolve nil: cadastro aberto.
func ParseSignUpEmails(list string) map[string]bool {
	allowed := map[string]bool{}
	for _, email := range strings.Split(list, ",") {
		if email = domain.NormalizeEmail(email); email != "" {
			allowed[email] = true
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	return allowed
}

// hashPassword e comparePassword são variáveis de pacote para o teste
// simular falha do bcrypt, que na prática não acontece com senha curta.
var (
	hashPassword = func(password []byte) ([]byte, error) {
		return bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	}
	comparePassword = bcrypt.CompareHashAndPassword
)

// loginGuard conta as senhas erradas seguidas por cliente (IP). Contador
// único travaria o dono sempre que alguém na internet errasse 5 vezes; por
// IP, quem erra trava só a si. Pedido local (sem IP) divide um contador só,
// o comportamento de antes. Fica na memória: reiniciar zera o contador.
type loginGuard struct {
	mu      sync.Mutex
	clients map[string]*clientAttempts
}

// clientAttempts são os erros seguidos de um cliente e até quando ele está
// travado.
type clientAttempts struct {
	failures    int
	lockedUntil time.Time
}

// attempts devolve o contador do cliente, criando se preciso. Com o mapa
// cheio, descarta quem não está travado: um IP travado nunca é esquecido
// antes do prazo (esquecer seria destravar).
func (g *loginGuard) attempts(client string, now time.Time) *clientAttempts {
	if g.clients == nil {
		g.clients = map[string]*clientAttempts{}
	}
	if found := g.clients[client]; found != nil {
		return found
	}
	if len(g.clients) >= maxTrackedClients {
		for key, state := range g.clients {
			if !now.Before(state.lockedUntil) {
				delete(g.clients, key)
			}
		}
	}
	created := &clientAttempts{}
	g.clients[client] = created
	return created
}

// dummyHash é o hash de uma senha descartável, gerado na partida. Com e-mail
// inexistente o bcrypt compara contra ele, gastando o mesmo tempo de uma
// conta real.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("nfse-digital-hash-de-tempo"), bcrypt.DefaultCost)

// SignUp cria uma conta nova — cada conta é uma empresa, com os próprios
// dados. Recusa e-mail que já tem conta e, com lista de autorizados, e-mail
// fora dela.
func (s *Service) SignUp(ctx context.Context, name, email, password string) (domain.Account, error) {
	if err := domain.ValidateSignUp(name, email, password); err != nil {
		return domain.Account{}, err
	}
	if s.SignUpEmails != nil && !s.SignUpEmails[domain.NormalizeEmail(email)] {
		return domain.Account{}, ErrSignUpClosed
	}
	hash, err := hashPassword([]byte(password))
	if err != nil {
		return domain.Account{}, fmt.Errorf("gerar hash da senha: %w", err)
	}
	account := domain.Account{Name: strings.TrimSpace(name), Email: domain.NormalizeEmail(email), PasswordHash: hash, CreatedAt: s.Now()}
	if err := s.Repo.CreateAccount(ctx, &account); err != nil {
		if errors.Is(err, ports.ErrConflict) {
			return domain.Account{}, ErrAccountExists
		}
		return domain.Account{}, fmt.Errorf("gravar conta: %w", err)
	}
	return account, nil
}

// Authenticate acha a conta pelo e-mail e confere a senha. O bcrypt roda
// mesmo com e-mail inexistente, para o tempo de resposta não revelar quais
// e-mails têm conta. O bloqueio conta por cliente (ports.Client), não por
// e-mail: por e-mail, quem ataca trocaria de e-mail a cada 4 erros.
func (s *Service) Authenticate(ctx context.Context, email, password string) (domain.Account, error) {
	s.guard.mu.Lock()
	defer s.guard.mu.Unlock()
	now := s.Now()
	guard := s.guard.attempts(ports.Client(ctx), now)
	if now.Before(guard.lockedUntil) {
		return domain.Account{}, ErrLocked
	}

	account, err := s.Repo.AccountByEmail(ctx, domain.NormalizeEmail(email))
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return domain.Account{}, err
	}
	hash := account.PasswordHash
	if len(hash) == 0 {
		hash = dummyHash
	}
	passwordOK := comparePassword(hash, []byte(password)) == nil
	if !passwordOK || domain.NormalizeEmail(email) != account.Email || account.Email == "" {
		guard.failures++
		if guard.failures >= maxFailedAttempts {
			guard.lockedUntil = now.Add(lockDuration)
			guard.failures = 0
		}
		return domain.Account{}, ErrInvalidCredentials
	}
	guard.failures = 0
	return account, nil
}

// DeleteAccount apaga a conta da operação e TODOS os dados dela (emitente,
// tomadores, modelos, notas, agendamentos, certificado selado, tokens de API
// e passkeys). Não tem volta: só acontece com o e-mail da conta digitado de
// novo, que separa a intenção de um clique por engano.
func (s *Service) DeleteAccount(ctx context.Context, confirmEmail string) error {
	account, err := s.Repo.Account(ctx)
	if err != nil {
		return err
	}
	if domain.NormalizeEmail(confirmEmail) != account.Email || account.Email == "" {
		return ErrDeleteNotConfirmed
	}
	return s.Repo.DeleteAccount(ctx)
}
