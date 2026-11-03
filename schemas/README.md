# Esquemas XSD da NFS-e nacional

| Item | Valor |
|---|---|
| Fonte | https://www.gov.br/nfse/pt-br/biblioteca/documentacao-tecnica/documentacao-atual/nfse-esquemas_xsd-v1-01-20260209.zip |
| Baixado em | 29/10/2026 |
| SHA-256 do zip | `e7935cbd9470527c6cc32984c1b2263e614183bf0139ce2733eaaed2de9a8072` |
| Versão usada | 1.01 (pasta `Schemas/1.01/` do zip) |

## Pastas

- `v1.01/` — cópia **intocada** dos arquivos oficiais.
- `v1.01-xmllint/` — cópia para validar com `xmllint`, com uma única mudança: os padrões
  `<xs:pattern value="^...$">` perderam o `^` inicial e o `$` final.

## Por que existe a cópia `v1.01-xmllint`

Na sintaxe de expressão regular do XML Schema, `^` e `$` **não são âncoras**: são caracteres
literais, e o padrão já casa a string inteira por definição. O esquema oficial usa `^...$`, então
um validador que siga a especificação exige um `^` de verdade no texto.

Medido em 29/10/2026: a NFS-e real nº 38 (autorizada pela própria SEFIN) **reprova** no esquema
oficial, em `serie = 1` contra o padrão `^0{0,4}\d{1,5}$`, e **passa** na cópia sem âncoras. Prova
de falha da cópia: o mesmo XML sem `<nDPS>` reprova (`Expected is ( nDPS )`, rc=3).

Comando:

```bash
nix shell nixpkgs#libxml2 --command xmllint --noout --schema schemas/v1.01-xmllint/DPS_v1.01.xsd testdata/dps-reference.xml
```

## Produção restrita (homologação) — pacote próprio

| Item | Valor |
|---|---|
| Fonte | https://www.gov.br/nfse/pt-br/biblioteca/documentacao-tecnica/producao-restrita/esquemas_prod_restrita_20261020.zip |
| Baixado em | 30/10/2026 |
| SHA-256 do zip | `25fdcecc651a67a216496ab10d7566631e6ed10f721e37d62c7845dadb0ea267` |
| Pastas | `prod-restrita-20261020/` (intocada) e `prod-restrita-20261020-xmllint/` (sem âncoras) |

A homologação valida a DPS contra este pacote, não contra o de produção (E1235 medido em
30/10/2026). Diferenças em `docs/sefin-notes.md`, seção 11.
