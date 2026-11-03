package nfsexml

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// cancellationCodes são os eventos que encerram a nota: e101101 é o
// cancelamento pelo emitente; e105102 é o cancelamento por substituição.
var cancellationCodes = []string{"e101101", "e105102"}

// ParseEvent lê um XML de evento da distribuição e devolve a chave da NFS-e
// afetada e se o evento a cancela.
func ParseEvent(data []byte) (accessKey string, cancels bool, err error) {
	var doc struct {
		XMLName xml.Name
		Key     string `xml:"infEvento>pedRegEvento>infPedReg>chNFSe"`
		AltKey  string `xml:"infPedReg>chNFSe"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return "", false, fmt.Errorf("%w: evento ilegível: %v", ErrNotNFSe, err)
	}
	accessKey = doc.Key + doc.AltKey
	if accessKey == "" {
		return "", false, fmt.Errorf("%w: evento sem chave da NFS-e", ErrNotNFSe)
	}
	for _, code := range cancellationCodes {
		if bytes.Contains(data, []byte("<"+code+">")) || bytes.Contains(data, []byte("<"+code+" ")) {
			return accessKey, true, nil
		}
	}
	return accessKey, false, nil
}
