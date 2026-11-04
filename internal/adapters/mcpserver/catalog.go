package mcpserver

import (
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpTool é o tipo de definição de ferramenta da SDK.
type mcpTool = mcp.Tool

// Skill é a skill do agente (SKILL.md), embutida para o painel oferecer o
// download e o texto nunca divergir do servidor que a acompanha.
//
//go:embed SKILL.md
var Skill string

// ToolInfo descreve uma ferramenta para quem lista (servidor e painel).
type ToolInfo struct {
	Name        string
	Description string
	// Writes marca a ferramenta que fala com a SEFIN em nome do emitente.
	Writes bool
}

// Tools é o catálogo único: New registra exatamente estas, na mesma ordem, e a
// tela de Integrações mostra esta mesma lista. Um teste confere os dois lados.
var Tools = []ToolInfo{
	{Name: "account_status", Description: "Mostra o emitente, o ambiente (homologação ou produção), a numeração da DPS, a validade do certificado e o que falta configurar."},
	{Name: "list_customers", Description: "Lista os tomadores cadastrados; filtra por nome ou documento."},
	{Name: "list_templates", Description: "Lista os modelos de serviço (código de tributação, valor e descrição padrão)."},
	{Name: "list_invoices", Description: "Lista as notas, da mais recente para a mais antiga."},
	{Name: "show_invoice", Description: "Mostra uma nota pelo ID, com status, chave de acesso e o erro da SEFIN traduzido."},
	{Name: "preview_invoice", Description: "Espelho da nota ANTES de emitir: mostra ambiente, tomador, modelo, competência, valor e a descrição integral, sem reservar número, gravar ou transmitir. Mostre ao dono e só chame emit_invoice depois do sim dele."},
	{Name: "emit_invoice", Writes: true, Description: "Emite uma NFS-e. Antes de chamar, mostre ao usuário o resumo (tomador, modelo, valor, competência) e espere a confirmação. Repetir a mesma idempotency_key devolve a nota já criada. Resposta pendente: consulte com show_invoice antes de pensar em emitir de novo."},
	{Name: "cancel_invoice", Writes: true, Description: "Cancela uma NFS-e autorizada na SEFIN. Irreversível: confirme com o usuário antes."},
	{Name: "download_danfse", Description: "Gera o DANFSe de uma nota autorizada ou cancelada e devolve o PDF e o XML autorizado, ambos em base64, com o nome de cada arquivo."},
	{Name: "list_schedules", Description: "Lista os agendamentos mensais com as próximas emissões."},
}

// tool monta a definição MCP pelo nome, lendo a descrição do catálogo. Nome
// fora do catálogo é defeito de programação e para na hora.
func tool(name string) *mcpTool {
	for _, info := range Tools {
		if info.Name == name {
			return &mcpTool{Name: info.Name, Description: info.Description}
		}
	}
	panic("ferramenta fora do catálogo: " + name)
}
