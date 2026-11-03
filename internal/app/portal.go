package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"nfse/internal/domain"
	"nfse/internal/ports"
)

// maxPortalBatches limita a varredura: 50 lotes de até 50 documentos (2.500)
// cobrem anos de notas de um emitente pequeno e impedem laço sem fim se o ADN
// insistir em responder "há mais".
const maxPortalBatches = 50

// PortalParsers são os leitores de XML usados na varredura; injetados para o
// núcleo não depender do adapter de XML.
type PortalParsers struct {
	NFSe  func([]byte) (domain.ImportedNFSe, error)
	Event func([]byte) (accessKey string, cancels bool, err error)
}

// PortalGroup é um serviço recorrente achado no Portal: mesmo tomador e mesmo
// código de serviço.
type PortalGroup struct {
	Key      string
	Customer domain.Customer
	Template domain.Template
	Count    int
	Monthly  bool
	LastNote domain.ImportedNFSe
	Known    bool
	notes    []domain.ImportedNFSe
}

// PortalScan é o resultado da varredura, antes de gravar qualquer coisa.
type PortalScan struct {
	Groups    []PortalGroup
	Emitted   int
	AsTaker   int
	Cancelled int
	LastNSU   int64
	// Batches é quantos lotes da distribuição foram lidos (a tela mostra).
	Batches int
}

// Recent devolve as n notas emitidas mais recentes da varredura, da mais
// nova para a mais antiga: é a lista "encontradas agora" da tela.
func (scan PortalScan) Recent(n int) []domain.ImportedNFSe {
	all := []domain.ImportedNFSe{}
	for _, group := range scan.Groups {
		all = append(all, group.notes...)
	}
	slices.SortFunc(all, func(a, b domain.ImportedNFSe) int { return b.Invoice.IssuedAt.Compare(a.Invoice.IssuedAt) })
	return all[:min(n, len(all))]
}

// ScanPortal lê toda a distribuição do CNPJ e agrupa as notas EMITIDAS pelo
// emitente (o ADN também distribui as notas em que ele é tomador). Nada é
// gravado: a escolha do que importar é do dono.
func (s *Service) ScanPortal(ctx context.Context, portal ports.PortalReader, parsers PortalParsers, issuerCNPJ string) (PortalScan, error) {
	scan := PortalScan{}
	emitted := []domain.ImportedNFSe{}
	cancelled := map[string]bool{}
	for page := 0; page < maxPortalBatches; page++ {
		batch, err := portal.Distribution(ctx, scan.LastNSU)
		if err != nil {
			return scan, err
		}
		scan.Batches++
		for _, document := range batch.Documents {
			scan.LastNSU = max(scan.LastNSU, document.NSU)
			switch document.Type {
			case "EVENTO":
				if key, cancels, err := parsers.Event(document.XML); err == nil && cancels {
					cancelled[key] = true
				}
			case "NFSE":
				imported, err := parsers.NFSe(document.XML)
				if err != nil {
					continue
				}
				if imported.Issuer.CNPJ != issuerCNPJ {
					scan.AsTaker++
					continue
				}
				emitted = append(emitted, imported)
			}
		}
		if !batch.More {
			break
		}
	}

	groups := map[string]*PortalGroup{}
	for _, note := range emitted {
		if cancelled[note.Invoice.AccessKey] {
			note.Invoice.Status = domain.StatusCancelled
			scan.Cancelled++
		}
		scan.Emitted++
		key := note.Customer.Document + "/" + note.Template.NationalTaxCode
		group, ok := groups[key]
		if !ok {
			group = &PortalGroup{Key: key, Customer: note.Customer}
			groups[key] = group
		}
		group.notes = append(group.notes, note)
		group.Count++
		if group.LastNote.Invoice.IssuedAt.Before(note.Invoice.IssuedAt) {
			group.LastNote = note
			group.Template = note.Template
		}
	}
	known, _ := s.Repo.Customers(ctx, "")
	for _, group := range groups {
		group.Monthly = monthly(group.notes)
		for _, customer := range known {
			group.Known = group.Known || customer.Document == group.Customer.Document
		}
		scan.Groups = append(scan.Groups, *group)
	}
	slices.SortFunc(scan.Groups, func(a, b PortalGroup) int { return b.Count - a.Count })
	return scan, nil
}

// monthly diz se as notas do grupo caem em meses consecutivos (pelo menos
// dois): é o critério de "todo mês" da tela.
func monthly(notes []domain.ImportedNFSe) bool {
	months := map[int]bool{}
	for _, note := range notes {
		date := note.Invoice.Competence
		months[date.Year()*12+int(date.Month())] = true
	}
	for month := range months {
		if months[month+1] {
			return true
		}
	}
	return false
}

// ApplyPortal grava os grupos escolhidos: tomador, modelo (com o nome do
// tomador) e todas as notas do grupo. É idempotente: rodar de novo com a
// mesma varredura não cria nada — nota casa pela chave de acesso, tomador
// pelo CPF/CNPJ e modelo pelo tomador + código do serviço. Importações da
// mesma conta esperam a anterior terminar.
func (s *Service) ApplyPortal(ctx context.Context, scan PortalScan, keys []string) (int, error) {
	lock, _ := s.importLocks.LoadOrStore(ports.AccountID(ctx), &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	imported := 0
	for _, group := range scan.Groups {
		if !slices.Contains(keys, group.Key) && !slices.Contains(keys, "all") {
			continue
		}
		// Da mais antiga para a mais recente: o modelo termina com os dados da última nota.
		slices.SortFunc(group.notes, func(a, b domain.ImportedNFSe) int { return a.Invoice.IssuedAt.Compare(b.Invoice.IssuedAt) })
		name := strings.TrimSpace(group.Customer.Name) + " — " + domain.FormatServiceCode(group.Template.NationalTaxCode)
		for _, note := range group.notes {
			report, err := s.Import(ctx, note, name, false)
			if err != nil {
				return imported, fmt.Errorf("grupo %s: %w", group.Key, err)
			}
			if strings.HasPrefix(report.Invoice, "importada") {
				imported++
			}
		}
	}
	return imported, nil
}
