package domain

// sefinMessages traduz os códigos de rejeição da SEFIN que o emitente já viu
// ou que aparecem como comuns na prática, com o que fazer em cada um.
var sefinMessages = map[string]string{
	"E0039": "O município do emitente não usa o emissor nacional da NFS-e. Esta nota precisa ser emitida no sistema da prefeitura.",
	"E0116": "O município exige a inscrição municipal do prestador. Preencha a IM nas configurações do emitente.",
	"E0120": "A inscrição municipal não deve ser informada neste ambiente. Comum em homologação: deixe a IM em branco para testar.",
	"E0160": "A situação no Simples Nacional não confere com o cadastro da Receita. Revise a opção pelo Simples nas configurações.",
	"E0166": "Falta o regime de apuração do Simples Nacional. Preencha o regime nas configurações do emitente.",
	"E0712": "O total de tributos não é compatível com o regime do emitente. Revise o regime nas configurações.",
	"E1831": "A SEFIN não encontrou esta NFS-e no Ambiente de Dados Nacional. Confira se a nota é deste ambiente (homologação ou produção).",
}

// TranslateSefinError devolve a explicação em português do código de
// rejeição; código desconhecido devolve a mensagem original da SEFIN.
func TranslateSefinError(code, original string) string {
	if message, ok := sefinMessages[code]; ok {
		return message
	}
	return original
}
