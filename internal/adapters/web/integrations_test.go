package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"nfse/internal/domain"
)

// TestIntegrationsTabs: cada aba mostra o comando do seu agente com o caminho
// do binário; ChatGPT e Grok explicam por que não conectam; aba desconhecida
// cai no Claude Code.
func TestIntegrationsTabs(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.server.UseIntegration(Integration{
		Binary: "/opt/nfse/bin/nfse",
		Tools:  []IntegrationTool{{Name: "list_invoices", Description: "lista as notas"}, {Name: "emit_invoice", Description: "emite", Writes: true}},
		Skill:  "# skill de teste",
	})
	cases := map[string][]string{
		"/integrations":                        {"claude mcp add nfse-digital -- /opt/nfse/bin/nfse mcp", "Baixar skill.md"},
		"/integrations/connect/claude/desktop": {`"command": "/opt/nfse/bin/nfse"`, "claude_desktop_config.json"},
		"/integrations/connect/codex":          {"codex mcp add nfse-digital -- /opt/nfse/bin/nfse mcp"},
		"/integrations/connect/chatgpt":        {"O ChatGPT se conecta", "Use Claude ou Codex"},
		"/integrations/connect/grok":           {"O Grok se conecta"},
		"/integrations/connect/claude":         {"claude mcp add nfse-digital"},
	}
	for path, wants := range cases {
		body := f.get(path).Body.String()
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s sem %q", path, want)
			}
		}
	}
	if body := f.get("/integrations/connect/chatgpt").Body.String(); strings.Contains(body, "Testar conexão") {
		t.Error("ChatGPT não deveria oferecer teste de conexão")
	}
	if body := f.get("/integrations/connect/codex").Body.String(); strings.Contains(body, "Claude Desktop</a>") {
		t.Error("abas do Claude apareceram no Codex")
	}
	body := f.get("/integrations/tools").Body.String()
	if !strings.Contains(body, "emit_invoice <span") || strings.Contains(body, "list_invoices <span") {
		t.Error("selo SEFIN errado nas ferramentas")
	}
}

// TestIntegrationsConnectionTest: sem contato, o teste avisa que nenhum agente
// falou; com contato gravado pelo MCP, mostra quem, qual ferramenta e quando,
// e a lista de agentes conectados aparece.
func TestIntegrationsConnectionTest(t *testing.T) {
	f := newFixture(t, nil, nil)
	if body := f.post("/integrations/test", url.Values{}).Body.String(); !strings.Contains(body, "Nenhum agente falou com o nfse ainda") ||
		!strings.Contains(f.get("/integrations/agents").Body.String(), "Nenhum agente conectado ainda") {
		t.Fatal("sem contato deveria avisar")
	}
	if body := f.get("/integrations").Body.String(); strings.Contains(body, "Nenhum agente falou") {
		t.Fatal("aviso do teste apareceu sem clicar em Testar conexão")
	}
	at := time.Date(2026, 10, 30, 1, 40, 0, 0, brt)
	_ = f.repo.RecordAgentContact(context.Background(), domain.AgentContact{Client: "claude-code", Tool: "account_status", At: at})
	body := f.post("/integrations/test", url.Values{"agent": {"claude"}, "client": {"code"}}).Body.String()
	if !strings.Contains(body, "Conectado: <strong>claude-code</strong> chamou <code>account_status</code> em 30/10/2026, 01:40") {
		t.Fatalf("contato não apareceu: %s", body)
	}
	if !strings.Contains(f.get("/integrations/agents").Body.String(), "<td>claude-code</td>") {
		t.Fatal("lista de agentes conectados sem o contato")
	}
}

// TestIntegrationSkillDownload: sem skill, 404; com skill, baixa como SKILL.md.
func TestIntegrationSkillDownload(t *testing.T) {
	f := newFixture(t, nil, nil)
	if response := f.get("/integrations/skill.md"); response.Code != http.StatusNotFound {
		t.Fatalf("sem skill: %d", response.Code)
	}
	if body := f.get("/integrations").Body.String(); !strings.Contains(body, "claude mcp add nfse-digital -- nfse mcp") || strings.Contains(body, "Baixar skill.md") {
		t.Fatal("sem dados: deveria usar \"nfse\" e esconder a skill")
	}
	f.server.UseIntegration(Integration{Skill: "# skill de teste"})
	skill := f.get("/integrations/skill.md")
	if skill.Code != http.StatusOK || skill.Body.String() != "# skill de teste" || !strings.Contains(skill.Header().Get("Content-Disposition"), "SKILL.md") {
		t.Fatalf("skill: %d %v", skill.Code, skill.Header())
	}
}

// TestIntegrationsSections: cada aba mostra só o seu bloco, com contagem ao
// lado do nome; aba desconhecida cai em "Conectar agente"; sem MCP por HTTP a
// aba de tokens some.
func TestIntegrationsSections(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.server.UseIntegration(Integration{Tools: []IntegrationTool{{Name: "list_invoices"}, {Name: "emit_invoice", Writes: true}}})
	_ = f.repo.RecordAgentContact(context.Background(), domain.AgentContact{Client: "codex", Tool: "list_invoices", At: f.server.now()})
	markers := map[string]string{"connect": "Testar conexão", "agents": "<td>codex</td>", "tools": "emit_invoice <span"}
	for section, own := range markers {
		body := f.get(sectionURL(section)).Body.String()
		if !strings.Contains(body, own) || !strings.Contains(body, `href="`+sectionURL(section)+`" class="tab tab-active"`) {
			t.Errorf("aba %s sem o próprio conteúdo ou sem destaque", section)
		}
		for other, marker := range markers {
			if other != section && strings.Contains(body, marker) {
				t.Errorf("aba %s mostrou o conteúdo de %s", section, other)
			}
		}
	}
	// Rota, não querystring: ?section= não troca de aba, e rota inventada é 404.
	body := f.get("/integrations?section=tools").Body.String()
	if !strings.Contains(body, "Testar conexão") || strings.Contains(body, "emit_invoice <span") {
		t.Error("querystring não deveria trocar de aba")
	}
	for _, path := range []string{"/integrations/qualquer", "/integrations/connect/outro", "/integrations/connect/claude/web", "/integrations/connect/codex/code"} {
		if response := f.get(path); response.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, response.Code)
		}
	}
	if !strings.Contains(body, `Ferramentas <span class="tabular text-xs text-muted-foreground">2</span>`) || !strings.Contains(body, `Agentes conectados <span class="tabular text-xs text-muted-foreground">1</span>`) {
		t.Error("contagem das abas")
	}
	if strings.Contains(body, `href="/integrations/tokens"`) || f.get("/integrations/tokens").Code != http.StatusNotFound {
		t.Error("sem MCP por HTTP, a aba de tokens não deveria existir")
	}
	f.server.UseMCP(http.NotFoundHandler())
	if !strings.Contains(f.get("/integrations").Body.String(), `href="/integrations/tokens"`) {
		t.Error("com MCP por HTTP, a aba de tokens deveria existir")
	}
}
