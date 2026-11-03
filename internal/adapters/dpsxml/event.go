package dpsxml

import (
	"crypto"

	"github.com/beevik/etree"

	"nfse/internal/domain"
)

// cancelDescription é o único valor que o XSD aceita no xDesc do e101101.
const cancelDescription = "Cancelamento de NFS-e"

// BuildCancel monta o pedido de registro do evento de cancelamento
// (pedRegEvento, TCPedRegEvt) na ordem do XSD. O leiaute do evento é o mesmo
// nos dois pacotes de esquema em vigor (comparado em 30/10/2026).
func BuildCancel(e domain.CancelEvent) *etree.Document {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	root := doc.CreateElement("pedRegEvento")
	root.CreateAttr("xmlns", Namespace)
	root.CreateAttr("versao", Version)

	inf := root.CreateElement("infPedReg")
	inf.CreateAttr("Id", e.ID)
	text(inf, "tpAmb", e.Environment.AmbientCode())
	text(inf, "verAplic", e.AppVersion)
	text(inf, "dhEvento", e.At.Format("2006-01-02T15:04:05-07:00"))
	text(inf, "CNPJAutor", e.AuthorCNPJ)
	text(inf, "chNFSe", e.AccessKey)
	cancel := inf.CreateElement("e" + domain.CancelEventType)
	text(cancel, "xDesc", cancelDescription)
	text(cancel, "cMotivo", e.ReasonCode)
	text(cancel, "xMotivo", e.Description)
	return doc
}

// EncodeCancel monta, assina o infPedReg, serializa e empacota o evento.
func EncodeCancel(e domain.CancelEvent, signer crypto.Signer, certificates [][]byte) (xml []byte, packed string, err error) {
	doc := BuildCancel(e)
	if err := signElement(doc, "infPedReg", signer, certificates); err != nil {
		return nil, "", err
	}
	if xml, err = Marshal(doc); err != nil {
		return nil, "", err
	}
	packed, err = Pack(xml)
	return xml, packed, err
}
