package sefin

import (
	"crypto"
	"errors"
	"io"
)

// failingSigner tem a chave pública certa e falha ao assinar, como uma
// aprovação negada no celular no meio da emissão.
type failingSigner struct{ crypto.Signer }

// Sign sempre falha.
func (failingSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("assinatura negada")
}
