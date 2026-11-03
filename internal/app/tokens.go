package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// tokenPrefix marca os tokens do emissor: quem achar um "nf_" num log sabe de
// onde ele veio e que precisa ser revogado.
const tokenPrefix = "nf_"

// tokenBytes é a entropia do token: 32 bytes (256 bits) tornam adivinhar
// inviável mesmo sem limite de tentativas.
const tokenBytes = 32

// ErrInvalidToken recusa token ausente, mal formado, revogado ou inexistente,
// sem dizer qual dos casos — a resposta não ajuda quem está adivinhando.
var ErrInvalidToken = errors.New("token de API inválido ou revogado")

// randomRead é a fonte de aleatoriedade; variável para o teste simular falha.
var randomRead = rand.Read

// HashToken é o que o banco guarda: SHA-256 em hexadecimal. Token de 256 bits
// aleatórios não precisa de sal nem de hash lento — não há dicionário.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IssueToken gera um token novo, grava só o hash e devolve o valor uma única
// vez: depois disso nem o painel consegue mostrá-lo de novo.
func (s *Service) IssueToken(ctx context.Context) (string, domain.APIToken, error) {
	raw := make([]byte, tokenBytes)
	if _, err := randomRead(raw); err != nil {
		return "", domain.APIToken{}, err
	}
	plain := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	token := domain.APIToken{Hint: plain[len(plain)-4:], CreatedAt: s.Now()}
	if err := s.Repo.CreateAPIToken(ctx, HashToken(plain), &token); err != nil {
		return "", domain.APIToken{}, err
	}
	return plain, token, nil
}

// VerifyToken confere o token pelo hash e registra o uso. Falha ao registrar
// o uso não recusa a chamada: o token é válido.
func (s *Service) VerifyToken(ctx context.Context, plain string) (domain.APIToken, error) {
	if !strings.HasPrefix(plain, tokenPrefix) {
		return domain.APIToken{}, ErrInvalidToken
	}
	token, err := s.Repo.APITokenByHash(ctx, HashToken(plain))
	if errors.Is(err, ports.ErrNotFound) {
		return domain.APIToken{}, ErrInvalidToken
	}
	if err != nil {
		return domain.APIToken{}, err
	}
	token.LastUsedAt = s.Now()
	_ = s.Repo.TouchAPIToken(ctx, token.ID, token.LastUsedAt)
	return token, nil
}
