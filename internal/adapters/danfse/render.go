// Package danfse gera o PDF do DANFSe localmente. A API de geração do ADN foi
// suspensa em 03/08/2026 (NT-008 v1.02, seção 1): desde então cabe ao
// software emissor imprimir o documento, em página única, com QR Code para a
// consulta pública.
package danfse

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	qrcode "github.com/skip2/go-qrcode"

	"nfse/internal/adapters/nfsexml"
	"nfse/internal/adapters/textsearch"
	"nfse/internal/domain"
)

// ConsultURL é a consulta pública que o QR Code abre (NT-008, seção 6.1).
const ConsultURL = "https://www.nfse.gov.br/ConsultaPublica/?tpc=1&chave="

// authenticityText vai abaixo do QR Code, como manda a NT-008.
const authenticityText = "A autenticidade desta NFS-e pode ser verificada pela leitura deste código QR ou pela consulta da chave de acesso no portal nacional da NFS-e"

// page é o estado do desenho: o documento, a tradução para cp1252 (as fontes
// base do PDF não têm UTF-8) e a posição vertical corrente.
type page struct {
	pdf *fpdf.Fpdf
	tr  func(string) string
	y   float64
}

// Medidas da página A4 em milímetros: margem de 10 e largura útil de 190.
const (
	left  = 10.0
	width = 190.0
)

// Render desenha o DANFSe e devolve os bytes do PDF.
func Render(d nfsexml.DANFSeData) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(left, 10, left)
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetTitle("DANFSe "+d.Number, true)
	pdf.AddPage()
	p := &page{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor(""), y: 10}

	p.header(d)
	p.section("PRESTADOR / EMITENTE", [][2]string{
		{"CNPJ/CPF", domain.FormatDocument(d.Provider.Document)}, {"Inscrição municipal", dash(d.Provider.Registration)},
		{"Nome / razão social", d.Provider.Name}, {"E-mail", dash(d.Provider.Email)},
		{"Endereço", dash(d.Provider.Address)}, {"Município", dash(d.Provider.City)},
	})
	p.section("TOMADOR DO SERVIÇO", [][2]string{
		{"CNPJ/CPF", domain.FormatDocument(d.Taker.Document)}, {"E-mail", dash(d.Taker.Email)},
		{"Nome / razão social", d.Taker.Name}, {"Município (IBGE)", dash(d.Taker.City)},
		{"Endereço", dash(d.Taker.Address)}, {"", ""},
	})
	p.section("SERVIÇO PRESTADO", [][2]string{
		{"Código de tributação nacional", domain.FormatServiceCode(d.ServiceCode) + " — " + d.ServiceName},
		{"Código de tributação municipal", dash(d.MunicipalTaxCode)},
		{"Local da prestação (IBGE)", dash(d.CityCode)}, {"Município de incidência", dash(d.IncidenceCity)},
	})
	p.text("DESCRIÇÃO DO SERVIÇO", d.Description, 22)
	p.section("TRIBUTAÇÃO MUNICIPAL (ISSQN)", [][2]string{
		{"Base de cálculo", money(d.BaseValue)}, {"Alíquota aplicada", dash(d.Rate) + " %"},
		{"Valor do ISSQN", money(d.ISSQN)}, {"Retenção do ISSQN", dash(d.Retention)},
	})
	p.section("IBS / CBS (REFORMA TRIBUTÁRIA)", [][2]string{
		{"Valor total da NF (com IBS/CBS)", money(d.IBSCBSTotal)}, {"IBS", money(d.IBSValue)},
		{"CBS", money(d.CBSValue)}, {"", ""},
	})
	p.total(d)
	p.text("INFORMAÇÕES COMPLEMENTARES", d.Complement, 18)
	if err := p.qr(d.AccessKey); err != nil {
		return nil, err
	}
	p.watermark(d.Watermark)
	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, fmt.Errorf("gerar PDF do DANFSe: %w", err)
	}
	return buffer.Bytes(), nil
}

// watermark escreve o texto em diagonal, cinza claro e semitransparente, no
// meio da página — por cima de tudo, para não dar para recortar fora.
func (p *page) watermark(text string) {
	if text == "" {
		return
	}
	pdf := p.pdf
	pdf.SetFont("Helvetica", "B", 44)
	pdf.SetTextColor(200, 40, 40)
	pdf.SetAlpha(0.25, "Normal")
	textWidth := pdf.GetStringWidth(p.tr(text))
	// Centro do A4 (210 x 297 mm); 45 graus deixam o texto inteiro na página.
	pdf.TransformBegin()
	pdf.TransformRotate(45, 105, 148.5)
	pdf.Text(105-textWidth/2, 148.5, p.tr(text))
	pdf.TransformEnd()
	pdf.SetAlpha(1, "Normal")
	pdf.SetTextColor(0, 0, 0)
}

// header é o cabeçalho: título, número, chave e datas.
func (p *page) header(d nfsexml.DANFSeData) {
	pdf := p.pdf
	pdf.SetFont("Helvetica", "B", 12)
	pdf.SetXY(left, p.y)
	pdf.CellFormat(width-45, 7, p.tr("DANFSe — Documento Auxiliar da NFS-e"), "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(45, 7, p.tr("NFS-e nº "+d.Number), "", 1, "R", false, 0, "")
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetX(left)
	pdf.CellFormat(width, 5, p.tr("Chave de acesso: "+groupKey(d.AccessKey)), "", 1, "L", false, 0, "")
	pdf.SetX(left)
	line := fmt.Sprintf("Competência %s · Emissão da DPS %s · Processamento %s · DPS %s, série %s · %s",
		date(d.Competence), dateTime(d.IssuedAt), dateTime(d.ProcessedAt), d.DPSNumber, d.DPSSeries, d.Environment)
	pdf.CellFormat(width, 5, p.tr(line), "", 1, "L", false, 0, "")
	p.y = pdf.GetY() + 2
}

// section desenha um quadro com título e pares rótulo/valor em duas colunas.
func (p *page) section(title string, fields [][2]string) {
	pdf := p.pdf
	rows := (len(fields) + 1) / 2
	height := 5 + float64(rows)*8
	pdf.Rect(left, p.y, width, height, "D")
	pdf.SetFont("Helvetica", "B", 7)
	pdf.SetXY(left+1, p.y+0.5)
	pdf.CellFormat(width-2, 4, p.tr(title), "", 0, "L", false, 0, "")
	for i, field := range fields {
		column, row := i%2, i/2
		x := left + 1 + float64(column)*(width/2)
		y := p.y + 5 + float64(row)*8
		pdf.SetFont("Helvetica", "", 6)
		pdf.SetXY(x, y)
		pdf.CellFormat(width/2-2, 3, p.tr(field[0]), "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetXY(x, y+3)
		pdf.CellFormat(width/2-2, 4, p.tr(clip(field[1], 55)), "", 0, "L", false, 0, "")
	}
	p.y += height + 1.5
}

// text desenha um quadro de texto livre com altura fixa.
func (p *page) text(title, body string, height float64) {
	pdf := p.pdf
	pdf.Rect(left, p.y, width, height, "D")
	pdf.SetFont("Helvetica", "B", 7)
	pdf.SetXY(left+1, p.y+0.5)
	pdf.CellFormat(width-2, 4, p.tr(title), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetX(left + 1)
	pdf.MultiCell(width-2, 3.6, p.tr(clip(dash(body), 600)), "", "L", false)
	p.y += height + 1.5
}

// total é o quadro do valor do serviço e do valor líquido, em destaque.
func (p *page) total(d nfsexml.DANFSeData) {
	pdf := p.pdf
	pdf.SetFillColor(240, 240, 240)
	pdf.Rect(left, p.y, width, 12, "FD")
	pdf.SetFont("Helvetica", "", 7)
	pdf.SetXY(left+1, p.y+1)
	pdf.CellFormat(width/2-2, 4, p.tr("VALOR DO SERVIÇO"), "", 0, "L", false, 0, "")
	pdf.CellFormat(width/2-2, 4, p.tr("VALOR LÍQUIDO DA NFS-e"), "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "B", 12)
	pdf.SetXY(left+1, p.y+5)
	pdf.CellFormat(width/2-2, 6, p.tr(money(d.Amount)), "", 0, "L", false, 0, "")
	pdf.CellFormat(width/2-2, 6, p.tr(money(d.NetValue)), "", 0, "L", false, 0, "")
	p.y += 13.5
}

// qr desenha o QR Code da consulta pública e o texto de autenticidade.
func (p *page) qr(accessKey string) error {
	png, err := qrcode.Encode(ConsultURL+accessKey, qrcode.Medium, 256)
	if err != nil {
		return fmt.Errorf("gerar QR Code: %w", err)
	}
	p.pdf.RegisterImageOptionsReader("qr", fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
	// 28 mm passa com folga do mínimo de 15,2 mm da NT-008.
	p.pdf.ImageOptions("qr", left, p.y, 28, 28, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	p.pdf.SetFont("Helvetica", "", 6)
	p.pdf.SetXY(left+31, p.y+8)
	p.pdf.MultiCell(70, 2.6, p.tr(authenticityText), "", "L", false)
	p.pdf.SetXY(left+31, p.y+18)
	p.pdf.CellFormat(150, 3, p.tr(ConsultURL+accessKey), "", 0, "L", false, 0, "")
	return nil
}

// FileName é o nome padrão do PDF: "nfse-<número>-<tomador>.pdf", com o nome
// do tomador sem acento, em minúsculas, só letras e dígitos separados por
// hífen e cortado em 40 caracteres para não estourar nome de anexo de e-mail.
func FileName(d nfsexml.DANFSeData) string {
	var slug strings.Builder
	pendingDash := false
	for _, r := range textsearch.Fold(d.Taker.Name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingDash && slug.Len() > 0 {
				slug.WriteByte('-')
			}
			slug.WriteRune(r)
			pendingDash = false
			continue
		}
		pendingDash = true
	}
	name := strings.TrimRight(clipASCII(slug.String(), 40), "-")
	if name == "" {
		return "nfse-" + d.Number + ".pdf"
	}
	return "nfse-" + d.Number + "-" + name + ".pdf"
}

// clipASCII corta texto ASCII sem reticências (é nome de arquivo).
func clipASCII(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// groupKey separa a chave de 50 dígitos em blocos de 4, mais legível impressa.
func groupKey(key string) string {
	var groups []string
	for len(key) > 4 {
		groups = append(groups, key[:4])
		key = key[4:]
	}
	return strings.Join(append(groups, key), " ")
}

// money formata "19000.00" como "R$ 19.000,00"; vazio vira travessão.
func money(value string) string {
	amount, err := domain.ParseBRL(value)
	if value == "" || err != nil {
		return "—"
	}
	return amount.FormatBRL()
}

// dash troca texto vazio por travessão.
func dash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

// clip corta texto longo com reticências, para não estourar o quadro.
func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

// date e dateTime formatam no padrão brasileiro; data zero vira travessão.
func date(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("02/01/2006")
}

// dateTime formata data e hora no fuso de Brasília.
func dateTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(domain.SaoPaulo()).Format("02/01/2006 15:04")
}
