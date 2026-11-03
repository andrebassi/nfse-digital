package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"nfse/internal/adapters/memory"
	"nfse/internal/adapters/simulated"
	"nfse/internal/domain"
	"nfse/internal/ports"
)

// password é a senha de teste usada em todos os cenários.
const password = "senha-de-teste-longa"

// newAuthService monta o serviço com relógio controlável pelo teste.
func newAuthService(now *time.Time) *Service {
	return &Service{Repo: memory.NewSeeded(brt), Emitter: simulated.Emitter{}, Location: brt, Now: func() time.Time { return *now }}
}

// TestSignUpManyAccounts: cada cadastro cria uma conta nova com hash; e-mail
// que já tem conta é recusado (com qualquer caixa de letra).
func TestSignUpManyAccounts(t *testing.T) {
	now := fixedNow()
	service := newAuthService(&now)
	ctx := context.Background()
	if accounts, _ := service.Repo.Accounts(ctx); len(accounts) != 0 {
		t.Fatal("seed não deveria ter conta")
	}
	account, err := service.SignUp(ctx, " Dona ", "Dona@Exemplo.Example", password)
	if err != nil || account.Email != "dona@exemplo.example" || account.Name != "Dona" {
		t.Fatalf("conta: %+v, %v", account, err)
	}
	if string(account.PasswordHash) == password || len(account.PasswordHash) == 0 {
		t.Fatal("senha não virou hash")
	}
	other, err := service.SignUp(ctx, "Outra", "outra@exemplo.example", password)
	if err != nil || other.ID == account.ID {
		t.Fatalf("segunda conta: %+v, %v", other, err)
	}
	if _, err := service.SignUp(ctx, "Repetida", "DONA@exemplo.example", password); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("e-mail repetido: %v", err)
	}
}

// TestSignUpValidation: senha curta volta como erro de campo, sem gravar nada.
func TestSignUpValidation(t *testing.T) {
	now := fixedNow()
	service := newAuthService(&now)
	_, err := service.SignUp(context.Background(), "Dona", "dona@exemplo.example", "curta")
	var fields domain.FieldErrors
	if !errors.As(err, &fields) || fields["password"] == "" {
		t.Fatalf("erro: %v", err)
	}
	if accounts, _ := service.Repo.Accounts(context.Background()); len(accounts) != 0 {
		t.Fatal("cadastro inválido gravou conta")
	}
}

// TestAuthenticate: senha certa entra; e-mail errado e senha errada dão a
// mesma mensagem; sem conta nenhuma também.
func TestAuthenticate(t *testing.T) {
	now := fixedNow()
	service := newAuthService(&now)
	ctx := context.Background()
	if _, err := service.Authenticate(ctx, "dona@exemplo.example", password); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("sem conta: %v", err)
	}
	_, _ = service.SignUp(ctx, "Dona", "dona@exemplo.example", password)
	if _, err := service.Authenticate(ctx, " DONA@exemplo.example", password); err != nil {
		t.Fatalf("login certo falhou: %v", err)
	}
	// Com duas contas, cada e-mail entra na própria.
	other, _ := service.SignUp(ctx, "Outra", "outra@exemplo.example", "outra-senha-longa")
	if account, err := service.Authenticate(ctx, "outra@exemplo.example", "outra-senha-longa"); err != nil || account.ID != other.ID {
		t.Fatalf("segunda conta: %+v, %v", account, err)
	}
	if _, err := service.Authenticate(ctx, "outra@exemplo.example", password); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("senha de outra conta: %v", err)
	}
	wrongEmail := func() error { _, err := service.Authenticate(ctx, "ninguem@exemplo.example", password); return err }()
	wrongPassword := func() error {
		_, err := service.Authenticate(ctx, "dona@exemplo.example", "errada-errada-errada")
		return err
	}()
	if !errors.Is(wrongEmail, ErrInvalidCredentials) || wrongEmail.Error() != wrongPassword.Error() {
		t.Fatalf("mensagens diferentes: %v / %v", wrongEmail, wrongPassword)
	}
}

// TestLockAfterFiveFailures: 5 erros seguidos travam por 5 minutos, até a
// senha certa é recusada no período, e depois volta a funcionar.
func TestLockAfterFiveFailures(t *testing.T) {
	now := fixedNow()
	service := newAuthService(&now)
	ctx := context.Background()
	_, _ = service.SignUp(ctx, "Dona", "dona@exemplo.example", password)
	for range 5 {
		_, _ = service.Authenticate(ctx, "dona@exemplo.example", "errada-errada-errada")
	}
	if _, err := service.Authenticate(ctx, "dona@exemplo.example", password); !errors.Is(err, ErrLocked) {
		t.Fatalf("deveria estar bloqueado: %v", err)
	}
	now = now.Add(5*time.Minute + time.Second)
	if _, err := service.Authenticate(ctx, "dona@exemplo.example", password); err != nil {
		t.Fatalf("depois do bloqueio: %v", err)
	}
}

// TestLockIsPerClient: os erros de um IP travam só aquele IP — o dono, em
// outro endereço, continua entrando. Com o mapa cheio, quem já destravou é
// esquecido e o IP travado continua travado.
func TestLockIsPerClient(t *testing.T) {
	now := fixedNow()
	service := newAuthService(&now)
	attacker := ports.WithClient(context.Background(), "203.0.113.9")
	owner := ports.WithClient(context.Background(), "198.51.100.7")
	_, _ = service.SignUp(owner, "Dona", "dona@exemplo.example", password)
	for range 5 {
		_, _ = service.Authenticate(attacker, "dona@exemplo.example", "errada-errada-errada")
	}
	if _, err := service.Authenticate(attacker, "dona@exemplo.example", password); !errors.Is(err, ErrLocked) {
		t.Fatalf("IP que errou deveria estar travado: %v", err)
	}
	if _, err := service.Authenticate(owner, "dona@exemplo.example", password); err != nil {
		t.Fatalf("o dono, em outro IP, foi travado junto: %v", err)
	}

	for i := range maxTrackedClients {
		service.guard.attempts(fmt.Sprintf("10.0.%d.%d", i/256, i%256), now)
	}
	service.guard.attempts("192.0.2.1", now)
	if len(service.guard.clients) > maxTrackedClients+1 {
		t.Fatalf("contador cresceu sem limite: %d", len(service.guard.clients))
	}
	if _, err := service.Authenticate(attacker, "dona@exemplo.example", password); !errors.Is(err, ErrLocked) {
		t.Fatalf("limpeza do mapa destravou o IP travado: %v", err)
	}
}

// TestSignUpAllowList: com lista, só e-mail da lista cria conta (com qualquer
// caixa de letra); sem lista, aberto. A lista vem do secret separada por
// vírgula, com espaços tolerados.
func TestSignUpAllowList(t *testing.T) {
	now := fixedNow()
	service := newAuthService(&now)
	service.SignUpEmails = ParseSignUpEmails(" Dona@Exemplo.example , ,outra@exemplo.example")
	if len(service.SignUpEmails) != 2 {
		t.Fatalf("lista: %v", service.SignUpEmails)
	}
	ctx := context.Background()
	if _, err := service.SignUp(ctx, "Estranho", "estranho@exemplo.example", password); !errors.Is(err, ErrSignUpClosed) {
		t.Fatalf("fora da lista: %v", err)
	}
	if _, err := service.SignUp(ctx, "Dona", "DONA@exemplo.example", password); err != nil {
		t.Fatalf("na lista: %v", err)
	}
	if ParseSignUpEmails("") != nil || ParseSignUpEmails(" , ") != nil {
		t.Fatal("lista vazia deveria deixar o cadastro aberto (nil)")
	}
}

// accountFailRepo falha na leitura e na gravação da conta.
type accountFailRepo struct {
	*memory.Repository
	failRead bool
}

// AccountByEmail falha com erro de disco quando failRead está ligado.
func (a accountFailRepo) AccountByEmail(ctx context.Context, email string) (domain.Account, error) {
	if a.failRead {
		return domain.Account{}, errors.New("disco")
	}
	return a.Repository.AccountByEmail(ctx, email)
}

// CreateAccount sempre falha.
func (accountFailRepo) CreateAccount(context.Context, *domain.Account) error {
	return errors.New("disco")
}

// TestAuthRepositoryFailures: erro de I/O e de hash viram erro, sem pânico.
func TestAuthRepositoryFailures(t *testing.T) {
	ctx := context.Background()
	readFail := &Service{Repo: accountFailRepo{Repository: memory.NewSeeded(brt), failRead: true}, Now: fixedNow}
	if _, err := readFail.Authenticate(ctx, "dona@exemplo.example", password); err == nil || errors.Is(err, ErrInvalidCredentials) {
		t.Error("Authenticate deveria devolver o erro de disco")
	}
	writeFail := &Service{Repo: accountFailRepo{Repository: memory.NewSeeded(brt)}, Now: fixedNow}
	if _, err := writeFail.SignUp(ctx, "Dona", "dona@exemplo.example", password); err == nil {
		t.Error("SignUp deveria falhar na gravação")
	}

	original := hashPassword
	defer func() { hashPassword = original }()
	hashPassword = func([]byte) ([]byte, error) { return nil, errors.New("bcrypt") }
	now := fixedNow()
	if _, err := newAuthService(&now).SignUp(ctx, "Dona", "dona@exemplo.example", password); err == nil {
		t.Error("falha do bcrypt deveria virar erro")
	}
}
