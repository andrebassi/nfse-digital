package domain

import "testing"

// TestValidateSignUp aceita cadastro completo e aponta cada campo errado.
func TestValidateSignUp(t *testing.T) {
	if err := ValidateSignUp("Dona", "dona@exemplo.example", "senha-bem-longa"); err != nil {
		t.Fatalf("cadastro válido recusado: %v", err)
	}
	fields := ValidateSignUp(" ", "sem-arroba", "curta").(FieldErrors)
	for _, field := range []string{"name", "email", "password"} {
		if fields[field] == "" {
			t.Errorf("faltou erro em %s", field)
		}
	}
	for _, email := range []string{"@exemplo.example", "dona@localhost"} {
		if ValidateSignUp("Dona", email, "senha-bem-longa") == nil {
			t.Errorf("e-mail %q passou", email)
		}
	}
	// Doze caracteres com acento contam como doze, não como bytes.
	if err := ValidateSignUp("Dona", "dona@exemplo.example", "ççççççççççç"); err == nil {
		t.Error("onze caracteres passaram")
	}
}

// TestNormalizeEmail tira espaço e maiúscula.
func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Dona@Exemplo.Example "); got != "dona@exemplo.example" {
		t.Fatalf("got %q", got)
	}
}
