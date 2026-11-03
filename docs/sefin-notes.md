# Notas da SEFIN Nacional — o que foi medido

Saída da story 2.1. Cada afirmação diz de onde veio: **XSD** (esquema oficial em `schemas/v1.01/`),
**XML real** (NFS-e nº 38, emitida em 05/10/2026 pelo Emissor Nacional, guardada fora do git em
`docs/reference/private/`) ou **a confirmar** (ainda sem prova).

## 1. Barueri usa o emissor nacional em produção

**XML real + Portal:** as notas nº 37, 38 e 39 (05/10/2026) foram emitidas pelo Emissor Nacional
com município emissor Barueri (`cLocEmi = 3505708`, `xLocEmi = BARUERI`), `tpAmb = 1` (produção),
`cStat = 100` (autorizada).

Consequência: a rejeição **E0039** da nota de 28/10/2026 veio da **produção restrita**
(homologação), onde a parametrização do município difere. A API nacional deve funcionar em
produção para este emitente; na homologação, a resposta esperada continua sendo E0039.

## 2. Namespace e versão

**XSD + XML real:** `xmlns="http://www.sped.fazenda.gov.br/nfse"`, atributo `versao="1.01"` em
`<DPS>` e `<NFSe>`.

## 3. Identificador da DPS — 45 posições

**XSD** (`TSIdDPS`, tiposSimples_v1.01.xsd):

```
"DPS" + Cód.Mun (7) + Tipo de Inscrição Federal (1) + Inscrição Federal (14) + Série (5) + Núm. DPS (15)
```

**XML real:** `DPS` + `3505708` + `2` (CNPJ) + CNPJ (14) + `00001` + `000000000202626`. Tipo de
inscrição **2 = CNPJ** (CPF completa com zeros à esquerda até 14).

## 4. Identificador da NFS-e — 53 posições

**XSD** (`TSIdNFSe`): `"NFS" + Cód.Mun.(7) + Amb.Ger.(1) + Tipo Insc.(1) + Inscrição (14) +
Nº NFS-e (13) + AnoMês Emissão (4) + Cód.Num.(9) + DV (1)`. A chave de acesso é esse Id sem o
`NFS` (50 dígitos).

## 5. Estrutura da DPS (campos usados pelo emitente)

**XML real**, `infDPS`, na ordem do XSD:

| Campo | Valor real | Observação |
|---|---|---|
| `tpAmb` | 1 | 1 produção, 2 homologação |
| `dhEmi` | `2026-10-05T19:22:06-03:00` | com fuso |
| `verAplic` | `Sistema NFS-e` | nós: `nfse-digital <versão>` |
| `serie` | `1` | sem zeros à esquerda no XML; no Id vai com 5 |
| `nDPS` | `202626` | idem, 15 no Id |
| `dCompet` | `2026-10-05` | data, não mês |
| `tpEmit` | 1 | prestador |
| `cLocEmi` | 3505708 | IBGE do emissor |
| `prest/CNPJ`, `prest/IM` | — | IM informada em produção |
| `prest/regTrib/opSimpNac` | 3 | ME/EPP |
| `prest/regTrib/regApTribSN` | 1 | federal e municipal pelo SN |
| `prest/regTrib/regEspTrib` | 0 | nenhum |
| `toma` | CNPJ, xNome, `end/endNac/cMun`, CEP, xLgr, nro, xCpl, xBairro, email | |
| `serv/locPrest/cLocPrestacao` | 3505708 | |
| `serv/cServ/cTribNac` | 010701 | |
| `serv/cServ/cTribMun` | 220 | |
| `serv/cServ/xDescServ` | texto | |
| `serv/cServ/cNBS` | 115013000 | |
| `serv/cServ/cIntContrib` | código interno | opcional |
| `serv/infoCompl/xInfComp` | texto | opcional |
| `valores/vServPrest/vServ` | `19000.00` | ponto decimal, 2 casas |
| `valores/trib/tribMun/tribISSQN` | 1 | |
| `valores/trib/tribMun/tpRetISSQN` | 1 | |
| `valores/trib/totTrib/indTotTrib` | 0 | |

## 6. Grupo IBS/CBS (reforma tributária) já presente

**XML real:** a DPS de outubro/2026 já traz `IBSCBS` — `finNFSe = 0`, `cIndOp = 050101`,
`indDest = 0`, `valores/trib/gIBSCBS/CST = 000`, `cClassTrib = 000001`. A NFS-e calculada volta com
`pIBSUF 0.10`, `pIBSMun 0.00`, `pCBS 0.90` (alíquotas de teste de 2026).

Consequência para a story 2.4: o modelo de serviço precisa guardar `cIndOp`, `CST` e `cClassTrib`,
e a DPS gerada precisa do grupo `IBSCBS`. Se ele é obrigatório para este emitente, **a confirmar**
no XSD (`TCRTCInfoIBSCBS`) e na primeira resposta da homologação.

## 7. Assinatura

**XML real:** a única assinatura do arquivo é a da SEFIN sobre `infNFSe`
(`Reference URI="#NFS..."`), com:

| Elemento | Algoritmo |
|---|---|
| CanonicalizationMethod | `http://www.w3.org/TR/2001/REC-xml-c14n-20010315` (C14N 1.0 inclusivo) |
| SignatureMethod | `http://www.w3.org/2000/09/xmldsig#rsa-sha1` |
| DigestMethod | `http://www.w3.org/2000/09/xmldsig#sha1` |
| Transforms | enveloped-signature + C14N 1.0 |

**XSD:** em `TCDPS` a `Signature` é opcional no esquema; em `TCNFSe` é obrigatória.

**A confirmar:** a DPS deste XML veio **sem assinatura** (foi gerada pelo emissor web). Que a DPS
enviada pela API usa os mesmos algoritmos é a hipótese de trabalho (é o que a SEFIN usa na NFS-e e
o que bibliotecas de terceiros usam); a prova é a primeira chamada real da story 2.7 — assinatura
errada volta E999 ou erro de assinatura, não E0039.

## 8. Defeito no XSD oficial: `^` e `$` nos padrões

Ver `schemas/README.md`. Validação local usa `schemas/v1.01-xmllint/`.

## 9. DANFSe

**Portal:** o emissor web oferece "Download DANFS-e" em
`/EmissorNacional/Notas/Download/DANFSe/{chave}` e "Download XML" em
`/EmissorNacional/Notas/Download/NFSe/{chave}`, ambos atrás de CAPTCHA. **A confirmar:** rota da
API (`GET /danfse/{chave}` na SEFIN ou no ADN) para a story 3.3.

## 10. Primeira conversa real com a SEFIN (homologação, 29–30/10/2026)

Seis envios da story 2.7 (`task smoke:homologation`, DPS 300001 a 300006, R$ 1,00, sem valor
fiscal). Cada um destravou o seguinte:

| DPS | Resposta | Causa | Conserto |
|---|---|---|---|
| — | `tls: no renegotiation` | o servidor pede o certificado de cliente por **renegociação TLS 1.2** | `Renegotiation: tls.RenegotiateOnceAsClient` |
| 300001 | E1235 `verAplic` | `TSVerAplic` aceita no máximo **20** caracteres | domínio corta em 20 |
| 300002 | E1235 `cLocEmi` fora de ordem | a homologação roda **outro leiaute** (ver seção 11) | gerador por ambiente |
| 300003 | E0116 | IM ausente | IM informada |
| 300004–6 | E0116 | a IM não consta no **cadastro (CNC) do município na produção restrita** | nenhum: dado do ambiente de teste |

**Provado contra a SEFIN real:** mTLS com o A1 do `pass` (aprovação no celular), esquema
aceito, **assinatura RSA-SHA1 + C14N 1.0 da DPS aceita** (fecha o "a confirmar" da seção 7).

**Formato real da resposta de erro** (HTTP 400):

```json
{"tipoAmbiente":2,"versaoAplicativo":"SefinNacional_1.6.0","dataHoraProcessamento":"2026-10-30T00:04:52-03:00",
 "idDPS":"DPS3505708…300005","erros":[{"Codigo":"E0116","Descricao":"A IM deve ser informada…"}]}
```

**Não há como chegar a "autorizada" na homologação com este emitente:** o cadastro de teste
do município não tem a empresa (E0116; o SaaS usado antes, com o leiaute antigo, recebia E0039). A
primeira autorização real só pode acontecer em produção, atrás da guarda da story 4.1.

## 11. Dois leiautes em vigor ao mesmo tempo

A produção restrita publicou em **20/10/2026** um pacote próprio
(`esquemas_prod_restrita_20261020.zip`, também "1.01") diferente do de produção (09/02/2026):

| Ponto | Produção (09/02) | Homologação (20/10) |
|---|---|---|
| `finNFSe` | dentro de `IBSCBS` | no `infDPS`, obrigatório, depois de `tpEmit` |
| ordem do `cServ` | `xDescServ` antes de `cNBS` | `xDescServ` **por último** |
| `IBSCBS/valores/trib` | `gIBSCBS{CST, cClassTrib}` | `CST, cClassTrib` direto no `trib` |
| `cIndOp` | obrigatório | opcional |

O gerador (`internal/adapters/dpsxml`) escolhe o leiaute pelo ambiente da nota, e o teste valida
cada um no XSD do seu ambiente **e** confere que ele reprova no do outro. Quando a produção adotar
o leiaute de 20/10, basta trocar a regra de escolha.

## 12. Rotas de leitura, distribuição e DANFSe (medidas em 30/10/2026)

| Rota | Resposta |
|---|---|
| `GET {sefin}/nfse/{chave}` | `{"nfseXmlGZipB64": …}` — o XML da nota |
| `GET {sefin}/dps/{IdDPS}` | `{"chaveAcesso": …}`; **404 E2404** = DPS sem NFS-e |
| `GET {adn}/contribuintes/DFe/{NSU}` | lote `LoteDFe[]` com `NSU`, `ChaveAcesso`, `TipoDocumento` (`NFSE`/`EVENTO`), `ArquivoXml` (gzip+base64); **E2220** = fim da distribuição |
| `GET {adn}/contribuintes/NFSe/{chave}/Eventos` | mesmo formato de lote, só os documentos da nota |
| `GET {sefin}/nfse/{chave}/eventos` | **405** — confirma que a rota existe e só aceita POST |
| `GET {adn}/danfse/{chave}` | **503** — API suspensa (abaixo) |

ADN em `https://adn.nfse.gov.br` (produção) e `https://adn.producaorestrita.nfse.gov.br`.

**TLS do ADN tem três armadilhas, todas medidas:**

1. redireciona para `http://` em algumas rotas — o leitor **não segue redirect**;
2. a lista de AC aceitas no handshake não casa com a cadeia do `.pfx`, então o Go não manda
   certificado nenhum (`handshake failure`) — `GetClientCertificate` devolve o A1 sempre;
3. em TLS 1.3 o servidor pede o certificado **depois** do handshake (post-handshake auth), que
   o Go não implementa (`bad record MAC`) — o leitor fica em TLS 1.2.

**DANFSe:** a NT-008 v1.02 suspendeu a geração pelo ADN em **03/08/2026**; o emissor gera o PDF
localmente, em página única, com QR Code para
`https://www.nfse.gov.br/ConsultaPublica/?tpc=1&chave=<chave>` (mínimo 15,2 mm). Fecha o
"a confirmar" da seção 9.

## 13. Evento de cancelamento (e101101)

`POST {sefin}/nfse/{chave}/eventos`, corpo `{"pedidoRegistroEventoXmlGZipB64": …}` com o
`pedRegEvento` assinado no `infPedReg` (mesmo assinador da DPS).

- `Id` = `PRE` + chave (50) + `101101` = **59 posições**. A documentação do `TSIdPedRegEvt`
  cita também o número do pedido, mas o **regex** do XSD não comporta: vale o regex.
- `cMotivo`: 1 erro na emissão, 2 serviço não prestado, 9 outros; `xMotivo` de 15 a 255.
- O leiaute do evento é igual nos dois pacotes de esquema (seção 11).

**Sonda real (homologação, 30/10/2026 00:36):** pedido assinado para uma chave fictícia do
emitente. Resposta HTTP 400 com **E1831** — *"a NFS-e indicada não existe no Ambiente de Dados
Nacional"*. A SEFIN só chega a essa regra depois de aceitar o campo JSON, o esquema e a
assinatura: os três ficam **provados contra o servidor real**. O caminho de sucesso (nota
cancelada de fato) **não** foi exercitado — não há nota autorizada em homologação (seção 10).
Note que a lista de erro aqui veio em `"erro"` (singular, minúsculo), não em `"erros"`.

## 14. A "nota de teste autorizada" do SaaS usado antes não veio da SEFIN Nacional (medido em 02/11/2026)

O SaaS usado antes mostra uma nota nº 2 (R$ 1,00, tomador CEDRO, 01/11/2026 19:21) como **autorizada
em homologação e cancelada**, chave `35057081211222333000181000000000000226111921531230`.
Com o nosso A1 (`task nfse:xml`, `nfse fetch`):

| Consulta | Resposta |
|---|---|
| `GET {sefin homologação}/nfse/{chave}` | **404** |
| `GET {sefin produção}/nfse/{chave}` | **404** |
| `GET {adn homologação}/contribuintes/NFSe/{chave}/Eventos` | **404 E2240** — nenhum documento para a chave |

A chave é **bem formada** (DV módulo 11 confere), e o 8º dígito é o **ambiente gerador = 1 —
Prefeitura** (`TSAmbGeradorNFSe`), não 2 — Sistema Nacional. A própria nota se apresenta como integração
do emissor com a Prefeitura de Barueri, em ambiente de testes, e usa série **RPS**, que é o
modelo do sistema **próprio do município**. Conclusão: o SaaS usado antes testou contra o ambiente de
testes da **prefeitura de Barueri**, não contra a produção restrita da SEFIN Nacional. Não há
campo a copiar: o E0116 da homologação nacional continua sendo dado do cadastro de teste (CNC),
como na seção 10.

No mesmo dia, o regime especial da conta estava gravado como **1 — ato cooperado** (a tela tinha
"Ato cooperado" e "Cooperativa" no mesmo código e faltavam 3, 4, 5 e 9). Corrigido para 0 —
nenhum; a reemissão (DPS 202632) voltou E0116 do mesmo jeito, então o regime não era a causa.

## 15. E0712: ME/EPP pela API precisa de pTotTribSN (produção, 02/11/2026)

Primeira emissão em produção pela API (R$ 1,00, CEDRO, DPS 202633): HTTP 400 **E0712** —
*"Para ME/EPP o indicador de informação de valor total de tributos não pode ser informado."*
Nenhuma nota criada.

A nota real nº 39 (05/10/2026, chave `…3926101943152034`, DPS **202627**) saiu pelo próprio
Emissor Nacional (`verAplic` = `Sistema NFS-e`) com `opSimpNac 3` **e** `indTotTrib 0`, e foi
autorizada. A regra, portanto, vale para quem emite pela API, não para o emissor web, que também
não guarda percentual nenhum: o valor tem de vir do emitente.

Consequência no código: para `opSimpNac 3` o `totTrib` sai como `pTotTribSN` (TSDec2V2, "6.00"),
com o percentual do emitente ("Alíquota aproximada do Simples (%)") ou, se preenchido, o
"% tributos Simples" do modelo. Sem percentual, a DPS de ME/EPP nem é montada. MEI e não optante
seguem com `indTotTrib 0`.

**Fechado no mesmo dia, em produção:** com `pTotTribSN 6.00` (alíquota informada pelo dono) e o
modelo 01.07.01, a DPS **202634** foi **autorizada** — NFS-e **nº 1**, chave
`35057082211222333000181000000000000126117485735299`. O 8º dígito (ambiente gerador) é **2 —
Sistema Nacional**, que numera à parte: as notas 32–39 são do emissor web (ambiente gerador 1),
por isso a primeira pela API é a nº 1. Cancelada em seguida pela CLI (`task cancel -- 30
--production`, motivo erro na emissão): resposta com `eventoXmlGZipB64` (o evento registrado), e o painel mostra
**cancelada**. É a primeira vez que o caminho de sucesso do evento e101101 roda de verdade
(a seção 13 só tinha a sonda com chave fictícia).
