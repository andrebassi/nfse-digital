package web

import (
	"net/http"
	"time"

	"nfse/internal/domain"
)

// IntegrationTool é uma ferramenta do servidor MCP, como a tela mostra.
type IntegrationTool struct {
	Name        string
	Description string
	// Writes marca a que fala com a SEFIN em nome do emitente.
	Writes bool
}

// Integration é o que a tela de Integrações precisa: o caminho do binário
// (para os comandos saírem prontos para colar), o catálogo de ferramentas e a
// skill do agente. Vem do cmd, que conhece o servidor MCP.
type Integration struct {
	Binary string
	Tools  []IntegrationTool
	Skill  string
}

// UseIntegration liga a tela de Integrações com os dados reais.
func (s *Server) UseIntegration(integration Integration) {
	s.integration = integration
}

// agentTab é uma aba de agente (ou de cliente dentro do Claude).
type agentTab struct {
	Key, Label, URL string
	Active          bool
	// Count é o número ao lado do nome (zero esconde).
	Count int
	// Disabled mostra a aba sem link (conta ainda sem certificado).
	Disabled bool
}

// integrationsView alimenta a tela: abas, o agente escolhido e o resultado do
// "Testar conexão".
type integrationsView struct {
	Integration
	Agent    string
	Client   string
	Agents   []agentTab
	Clients  []agentTab
	Tested   bool
	Contacts []domain.AgentContact
	Now      time.Time
	// BaseURL é o endereço do painel; MCPURL, o do servidor MCP por HTTP.
	BaseURL string
	MCPURL  string
	// HTTP diz se o MCP por HTTP (com token) está ligado neste painel.
	HTTP bool
	// Token é o valor recém-gerado: só existe na resposta do "Gerar token".
	Token string
	// Tokens são os tokens existentes (sem o valor).
	Tokens []domain.APIToken
	// Section é a aba da página; Sections, as abas com contagem.
	Section  string
	Sections []agentTab
	// APIOp é a operação de exemplo da aba API REST; APIOps, as abas dela.
	APIOp  string
	APIOps []agentTab
	// APICustomerID e APITemplateID entram no exemplo de emissão: o primeiro
	// tomador e o primeiro modelo da conta, para o curl sair pronto para rodar.
	APICustomerID int64
	APITemplateID int64
}

// agents são as abas de primeiro nível, na ordem da tela.
var agents = []agentTab{{Key: "claude", Label: "Claude"}, {Key: "chatgpt", Label: "ChatGPT"}, {Key: "grok", Label: "Grok"}, {Key: "codex", Label: "Codex"}}

// sections são as abas da página, na ordem da tela.
var sections = []agentTab{{Key: "connect", Label: "Conectar agente"}, {Key: "api", Label: "API REST"}, {Key: "tokens", Label: "Tokens de API"}, {Key: "agents", Label: "Agentes conectados"}, {Key: "tools", Label: "Ferramentas"}}

// apiOps são as operações de exemplo da aba API REST, na ordem da tela.
var apiOps = []agentTab{{Key: "emit", Label: "Emitir"}, {Key: "show", Label: "Consultar"}, {Key: "list", Label: "Listar"}, {Key: "cancel", Label: "Cancelar"}}

// claudeClients são as abas de cliente dentro do Claude.
var claudeClients = []agentTab{{Key: "code", Label: "Claude Code"}, {Key: "desktop", Label: "Claude Desktop"}}

// integrationsPage diz qual aba e qual agente mostrar. Vem da rota (ou do
// formulário, nos POST de gerar token e testar conexão), nunca de querystring.
type integrationsPage struct {
	Section, Agent, Client string
	// APIOp é a operação de exemplo, só na aba API REST.
	APIOp  string
	Token  string
	Tested bool
}

// sectionURL é a rota de cada aba da página.
func sectionURL(key string) string {
	if key == "connect" {
		return "/integrations"
	}
	return "/integrations/" + key
}

// connectURL é a rota da aba de um agente (e do cliente, no Claude).
func connectURL(agent, client string) string {
	if agent == "claude" {
		return "/integrations/connect/claude/" + client
	}
	return "/integrations/connect/" + agent
}

// integrations abre a aba Conectar agente no Claude Code.
func (s *Server) integrations(w http.ResponseWriter, r *http.Request) {
	s.renderIntegrations(w, r, integrationsPage{Section: "connect", Agent: "claude", Client: "code"})
}

// integrationsConnect abre a aba de um agente pela rota
// /integrations/connect/{agent}[/{client}]; agente ou cliente fora da lista é 404.
func (s *Server) integrationsConnect(w http.ResponseWriter, r *http.Request) {
	agent, client := r.PathValue("agent"), r.PathValue("client")
	if agent == "claude" && client == "" {
		client = "code"
	}
	if !known(agent, agents) || (agent == "claude" && !known(client, claudeClients)) || (agent != "claude" && client != "") {
		http.NotFound(w, r)
		return
	}
	s.renderIntegrations(w, r, integrationsPage{Section: "connect", Agent: agent, Client: client})
}

// integrationsSection abre as abas Tokens, Agentes e Ferramentas pela rota.
func (s *Server) integrationsSection(section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if section == "tokens" && s.mcpHandler == nil {
			http.NotFound(w, r)
			return
		}
		s.renderIntegrations(w, r, integrationsPage{Section: section, Agent: "claude", Client: "code"})
	}
}

// integrationsAPI abre a aba API REST pela rota /integrations/api[/{op}];
// operação fora da lista é 404. Sem MCP por HTTP não há token, e sem token a
// API não abre: a aba some, como a de Tokens.
func (s *Server) integrationsAPI(w http.ResponseWriter, r *http.Request) {
	op := r.PathValue("op")
	if op == "" {
		op = apiOps[0].Key
	}
	if s.mcpHandler == nil || !known(op, apiOps) {
		http.NotFound(w, r)
		return
	}
	s.renderIntegrations(w, r, integrationsPage{Section: "api", APIOp: op, Agent: "claude", Client: "code"})
}

// integrationsTest é o "Testar conexão": volta para a aba do agente de onde
// o botão foi clicado, agora com o resultado.
func (s *Server) integrationsTest(w http.ResponseWriter, r *http.Request) {
	page := formPage(r)
	page.Tested = true
	s.renderIntegrations(w, r, page)
}

// formPage lê agente e cliente do formulário, com o Claude Code como padrão.
// O "Gerar token" da aba API REST manda section=api e a operação, para a
// página voltar nela com o token já nos exemplos.
func formPage(r *http.Request) integrationsPage {
	if r.PostFormValue("section") == "api" {
		return integrationsPage{Section: "api", APIOp: pick(r.PostFormValue("op"), apiOps), Agent: "claude", Client: "code"}
	}
	agent := pick(r.PostFormValue("agent"), agents)
	client := ""
	if agent == "claude" {
		client = pick(r.PostFormValue("client"), claudeClients)
	}
	return integrationsPage{Section: "connect", Agent: agent, Client: client}
}

// renderIntegrations monta a tela da aba pedida.
func (s *Server) renderIntegrations(w http.ResponseWriter, r *http.Request, page integrationsPage) {
	view := integrationsView{
		Integration: s.integration, Agent: page.Agent, Client: page.Client, Tested: page.Tested, Now: s.now(), Section: page.Section,
		BaseURL: s.baseURL, MCPURL: s.baseURL + "/mcp", HTTP: s.mcpHandler != nil, Token: page.Token,
		APIOp: page.APIOp, APICustomerID: 1, APITemplateID: 1,
	}
	for _, tab := range apiOps {
		tab.URL, tab.Active = "/integrations/api/"+tab.Key, tab.Key == view.APIOp
		view.APIOps = append(view.APIOps, tab)
	}
	if view.Binary == "" {
		view.Binary = "nfse"
	}
	for _, tab := range agents {
		client := ""
		if tab.Key == "claude" {
			client = "code"
		}
		tab.URL, tab.Active = connectURL(tab.Key, client), tab.Key == view.Agent
		view.Agents = append(view.Agents, tab)
	}
	for _, tab := range claudeClients {
		tab.URL, tab.Active = connectURL("claude", tab.Key), tab.Key == view.Client
		view.Clients = append(view.Clients, tab)
	}
	contacts, err := s.repo.AgentContacts(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	view.Contacts = contacts
	if view.Section == "api" {
		if customers, err := s.repo.Customers(r.Context(), ""); err == nil && len(customers) > 0 {
			view.APICustomerID = customers[0].ID
		}
		if templates, err := s.repo.Templates(r.Context(), ""); err == nil && len(templates) > 0 {
			view.APITemplateID = templates[0].ID
		}
	}
	if view.Tokens, err = s.repo.APITokens(r.Context()); err != nil {
		fail(w, err)
		return
	}
	// Contagem ao lado do nome da aba, como nas abas de notas.
	counts := map[string]int{"tokens": len(view.Tokens), "agents": len(view.Contacts), "tools": len(view.Tools)}
	for _, tab := range sections {
		if (tab.Key == "tokens" || tab.Key == "api") && s.mcpHandler == nil {
			continue
		}
		tab.URL, tab.Active, tab.Count = sectionURL(tab.Key), tab.Key == view.Section, counts[tab.Key]
		view.Sections = append(view.Sections, tab)
	}
	s.render(w, r, http.StatusOK, "integrations", "Integrações", view)
}

// known diz se a chave é uma das abas.
func known(key string, tabs []agentTab) bool {
	for _, tab := range tabs {
		if tab.Key == key {
			return true
		}
	}
	return false
}

// pick devolve a chave pedida se ela for uma das abas; senão, a primeira.
func pick(key string, tabs []agentTab) string {
	for _, tab := range tabs {
		if tab.Key == key {
			return key
		}
	}
	return tabs[0].Key
}

// integrationSkill entrega a skill do agente como arquivo SKILL.md.
func (s *Server) integrationSkill(w http.ResponseWriter, r *http.Request) {
	if s.integration.Skill == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="SKILL.md"`)
	_, _ = w.Write([]byte(s.integration.Skill))
}
