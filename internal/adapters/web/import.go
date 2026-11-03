package web

import (
	"io"
	"net/http"

	"nfse/internal/adapters/nfsexml"
	"nfse/internal/domain"
)

// maxPostSize limita qualquer POST, upload incluído: uma NFS-e tem poucos KB;
// 2 MB sobra e impede que um arquivo enorme ocupe a memória do painel.
const maxPostSize = 2 << 20

// importView são os dados da tela de importação.
type importView struct {
	TemplateName string
	Overwrite    bool
	Error        string
	Report       *domain.ImportReport
}

// importForm mostra o formulário de upload do XML.
func (s *Server) importForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "import", "Importar XML", importView{})
}

// importSubmit lê o XML enviado, interpreta e grava os cadastros e a nota.
func (s *Server) importSubmit(w http.ResponseWriter, r *http.Request) {
	view := importView{TemplateName: r.PostFormValue("template_name"), Overwrite: r.PostFormValue("overwrite_issuer") == "on"}
	file, _, err := r.FormFile("xml")
	if err != nil {
		view.Error = "escolha o arquivo XML da nota (até 2 MB)"
		s.render(w, r, http.StatusUnprocessableEntity, "import", "Importar XML", view)
		return
	}
	defer file.Close()
	data, _ := io.ReadAll(file)
	imported, err := nfsexml.Parse(data)
	if err != nil {
		view.Error = err.Error()
		s.render(w, r, http.StatusUnprocessableEntity, "import", "Importar XML", view)
		return
	}
	report, err := s.service.Import(r.Context(), imported, view.TemplateName, view.Overwrite)
	if err != nil {
		view.Error = err.Error()
		s.render(w, r, http.StatusUnprocessableEntity, "import", "Importar XML", view)
		return
	}
	view.Report = &report
	s.render(w, r, http.StatusOK, "import", "Importar XML", view)
}
