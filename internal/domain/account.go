package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// MinPasswordLength é o mínimo de caracteres da senha. Doze, porque o painel
// não tem segundo fator até a passkey chegar (story 4.4).
const MinPasswordLength = 12

// Account é a conta única do dono do emissor. Guarda só o hash da senha.
type Account struct {
	ID           int64
	Name         string
	Email        string
	PasswordHash []byte
	CreatedAt    time.Time
}

// NormalizeEmail tira espaços e passa para minúsculas, para o login aceitar
// o e-mail digitado com maiúscula.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateSignUp confere o cadastro antes de gerar o hash: nome, e-mail com
// arroba e domínio, e senha com o mínimo de caracteres.
func ValidateSignUp(name, email, password string) error {
	errs := FieldErrors{}
	if strings.TrimSpace(name) == "" {
		errs["name"] = "informe o seu nome"
	}
	local, domain, found := strings.Cut(NormalizeEmail(email), "@")
	if !found || local == "" || !strings.Contains(domain, ".") {
		errs["email"] = "e-mail inválido"
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		errs["password"] = "a senha precisa de pelo menos 12 caracteres"
	}
	return errs.orNil()
}
