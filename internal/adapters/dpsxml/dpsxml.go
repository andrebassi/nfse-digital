// Package dpsxml transforma a DPS do domínio no XML da SEFIN (leiaute 1.01),
// assina com o certificado A1 e empacota em gzip+base64 para a API.
package dpsxml

import (
	"bytes"
	"compress/gzip"
	"crypto"
	"encoding/base64"
	"fmt"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"

	"nfse/internal/domain"
)

// Namespace e Version são os do leiaute vigente (schemas/v1.01).
const (
	Namespace = "http://www.sped.fazenda.gov.br/nfse"
	Version   = "1.01"
)

// Algoritmos da assinatura, os mesmos que a SEFIN usa na NFS-e real
// (docs/sefin-notes.md, seção 7): RSA-SHA1 sobre C14N 1.0 inclusivo.
const (
	SignatureMethod = "http://www.w3.org/2000/09/xmldsig#rsa-sha1"
)

// Build monta o documento XML da DPS na ordem exata do XSD. A ordem importa:
// o esquema é uma sequência, e elemento fora de ordem reprova.
//
// Os dois ambientes rodam leiautes diferentes (medido em 30/10/2026, ver
// docs/sefin-notes.md): a produção usa o pacote 1.01 de 09/02/2026 e a
// homologação, o de 20/10/2026, que move finNFSe para o infDPS, põe xDescServ
// no fim do cServ e troca a estrutura do trib do IBS/CBS.
func Build(d domain.DPS) *etree.Document {
	restricted := d.Environment == domain.EnvHomologation
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	root := doc.CreateElement("DPS")
	root.CreateAttr("xmlns", Namespace)
	root.CreateAttr("versao", Version)

	inf := root.CreateElement("infDPS")
	inf.CreateAttr("Id", d.ID)
	text(inf, "tpAmb", d.Environment.AmbientCode())
	text(inf, "dhEmi", d.IssuedAt.Format("2006-01-02T15:04:05-07:00"))
	text(inf, "verAplic", d.AppVersion)
	text(inf, "serie", d.Series)
	text(inf, "nDPS", fmt.Sprint(d.Number))
	text(inf, "dCompet", d.Competence.Format("2006-01-02"))
	text(inf, "tpEmit", "1")
	if restricted {
		text(inf, "finNFSe", "0")
	}
	text(inf, "cLocEmi", d.CityCode)

	prest := inf.CreateElement("prest")
	text(prest, "CNPJ", d.Provider.CNPJ)
	optional(prest, "IM", d.Provider.MunicipalRegistration)
	regTrib := prest.CreateElement("regTrib")
	text(regTrib, "opSimpNac", d.Provider.SimplesOption)
	optional(regTrib, "regApTribSN", d.Provider.SimplesRegime)
	text(regTrib, "regEspTrib", d.Provider.SpecialRegime)

	toma := inf.CreateElement("toma")
	documentTag := "CNPJ"
	if d.Taker.DocumentKind == domain.KindCPF {
		documentTag = "CPF"
	}
	text(toma, documentTag, d.Taker.Document)
	text(toma, "xNome", d.Taker.Name)
	if address := d.Taker.Address; address != nil {
		end := toma.CreateElement("end")
		endNac := end.CreateElement("endNac")
		text(endNac, "cMun", address.CityCode)
		text(endNac, "CEP", address.ZIP)
		text(end, "xLgr", address.Street)
		text(end, "nro", address.Number)
		optional(end, "xCpl", address.Complement)
		text(end, "xBairro", address.District)
	}
	optional(toma, "email", d.Taker.Email)

	serv := inf.CreateElement("serv")
	text(serv.CreateElement("locPrest"), "cLocPrestacao", d.Service.CityCode)
	cServ := serv.CreateElement("cServ")
	text(cServ, "cTribNac", d.Service.NationalTaxCode)
	optional(cServ, "cTribMun", d.Service.MunicipalTaxCode)
	if restricted {
		optional(cServ, "cNBS", d.Service.NBS)
		text(cServ, "xDescServ", d.Service.Description)
	} else {
		text(cServ, "xDescServ", d.Service.Description)
		optional(cServ, "cNBS", d.Service.NBS)
	}

	valores := inf.CreateElement("valores")
	text(valores.CreateElement("vServPrest"), "vServ", d.Values.Amount.FormatDecimal())
	trib := valores.CreateElement("trib")
	tribMun := trib.CreateElement("tribMun")
	text(tribMun, "tribISSQN", d.Values.ISSQNTaxation)
	text(tribMun, "tpRetISSQN", d.Values.ISSQNRetention)
	// totTrib é obrigatório e é uma escolha: ME/EPP manda o percentual do
	// Simples (pTotTribSN) — indTotTrib ali volta E0712, medido em produção em
	// 02/11/2026; os outros regimes mandam indTotTrib 0 (não informar).
	totTrib := trib.CreateElement("totTrib")
	if d.Values.SimplesTaxRate != "" {
		text(totTrib, "pTotTribSN", d.Values.SimplesTaxRate)
	} else {
		text(totTrib, "indTotTrib", "0")
	}

	ibscbs := inf.CreateElement("IBSCBS")
	if !restricted {
		text(ibscbs, "finNFSe", "0")
	}
	text(ibscbs, "cIndOp", d.IBSCBS.Operation)
	text(ibscbs, "indDest", "0")
	ibscbsTrib := ibscbs.CreateElement("valores").CreateElement("trib")
	if !restricted {
		ibscbsTrib = ibscbsTrib.CreateElement("gIBSCBS")
	}
	text(ibscbsTrib, "CST", d.IBSCBS.CST)
	text(ibscbsTrib, "cClassTrib", d.IBSCBS.Class)
	return doc
}

// text cria um elemento filho com texto.
func text(parent *etree.Element, tag, value string) {
	parent.CreateElement(tag).SetText(value)
}

// optional cria o elemento só quando há valor: campo opcional vazio reprova
// no XSD (os padrões exigem pelo menos um caractere).
func optional(parent *etree.Element, tag, value string) {
	if value != "" {
		text(parent, tag, value)
	}
}

// Sign assina o infDPS e põe a <Signature> como último filho de <DPS>, irmã
// do infDPS, que é onde o XSD (TCDPS) a espera.
func Sign(doc *etree.Document, signer crypto.Signer, certificates [][]byte) error {
	return signElement(doc, "infDPS", signer, certificates)
}

// signElement assina o filho `name` da raiz e acrescenta a <Signature> como
// irmã dele. DPS (infDPS) e pedido de evento (infPedReg) seguem o mesmo molde.
func signElement(doc *etree.Document, name string, signer crypto.Signer, certificates [][]byte) error {
	ctx, err := dsig.NewSigningContext(signer, certificates)
	if err != nil {
		return fmt.Errorf("preparar assinatura: %w", err)
	}
	if err := ctx.SetSignatureMethod(SignatureMethod); err != nil {
		return err
	}
	ctx.Canonicalizer = dsig.MakeC14N10RecCanonicalizer()
	ctx.IdAttribute = "Id"
	// Sem prefixo "ds:": a nota real traz <Signature xmlns="...xmldsig#">.
	ctx.Prefix = ""
	root := doc.Root()
	inf := root.SelectElement(name)
	signature, err := ctx.ConstructSignature(inf, true)
	if err != nil {
		return fmt.Errorf("assinar %s: %w", name, err)
	}
	root.AddChild(signature)
	return nil
}

// Marshal serializa sem indentação. Espaço ou quebra de linha a mais depois
// de assinar muda o digest e invalida a assinatura.
func Marshal(doc *etree.Document) ([]byte, error) {
	doc.WriteSettings.CanonicalText = true
	return doc.WriteToBytes()
}

// Pack comprime com gzip e codifica em base64, o formato do campo
// dpsXmlGZipB64 da API.
func Pack(xml []byte) (string, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(xml); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buffer.Bytes()), nil
}

// Encode faz o caminho inteiro: monta, assina, serializa e empacota.
func Encode(d domain.DPS, signer crypto.Signer, certificates [][]byte) (xml []byte, packed string, err error) {
	doc := Build(d)
	if err := Sign(doc, signer, certificates); err != nil {
		return nil, "", err
	}
	if xml, err = Marshal(doc); err != nil {
		return nil, "", err
	}
	packed, err = Pack(xml)
	return xml, packed, err
}
