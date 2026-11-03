package domain

// ImportedNFSe é o que se aproveita de um XML de NFS-e (ou de DPS) já
// emitida: emitente, tomador, modelo de serviço e a própria nota.
type ImportedNFSe struct {
	Issuer   Issuer
	Customer Customer
	Template Template
	Invoice  Invoice
	// HasSignature diz se o arquivo trazia assinatura; o XML importado é só
	// fonte de dados, então ela não é conferida, mas fica registrada.
	HasSignature bool
}

// ImportReport diz o que a importação fez com cada parte, para a tela e a CLI.
type ImportReport struct {
	Issuer   string
	Customer string
	Template string
	Invoice  string
}
