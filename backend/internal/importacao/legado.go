package importacao

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/imagem"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/validacao"
)

// ArquivoLegado é o JSON exportado pelo protótipo HTML.
type ArquivoLegado struct {
	Versao int           `json:"versao"`
	Cupons []CupomLegado `json:"cupons"`
}

// CupomLegado espelha o formato do protótipo, com os nomes curtos originais.
type CupomLegado struct {
	ID      string       `json:"id"`
	Data    string       `json:"data"`
	Num     string       `json:"num"`
	Total   float64      `json:"total"`
	Emp     string       `json:"emp"`
	CNPJEmp string       `json:"cnpjEmp"`
	Cli     string       `json:"cli"`
	CNPJCli string       `json:"cnpjCli"`
	Itens   []ItemLegado `json:"itens"`
	Foto    string       `json:"foto"` // data URL base64
	Fonte   string       `json:"fonte"`
	Obs     string       `json:"obs"`
	Status  string       `json:"status"` // ausente = confirmado
	Inc     []string     `json:"inc"`
	Avisos  []string     `json:"avisos"`
}

// ItemLegado é uma linha de item no formato do protótipo.
type ItemLegado struct {
	Cod  string   `json:"cod"`
	Prod string   `json:"prod"`
	Desc string   `json:"desc"`
	Un   string   `json:"un"`
	Qtd  float64  `json:"qtd"`
	Vun  float64  `json:"vun"`
	Vtot float64  `json:"vtot"`
	Inc  []string `json:"inc"`
}

// ResumoLegado é o resultado da importação do legado.
type ResumoLegado struct {
	Arquivo     string   `json:"arquivo"`
	Total       int      `json:"total"`
	Criados     int      `json:"criados"`
	Atualizados int      `json:"atualizados"`
	Ignorados   int      `json:"ignorados"`
	Erros       []string `json:"erros"`
	// Conferencia traz a soma por cliente/status, para comparar com o
	// critério de aceite do CLAUDE.md §10.
	Conferencia []ConferenciaCliente `json:"conferencia"`
}

// ConferenciaCliente é a soma importada de um cliente.
type ConferenciaCliente struct {
	Cliente         string        `json:"cliente"`
	CNPJ            string        `json:"cnpj"`
	Confirmados     int           `json:"confirmados"`
	Pendentes       int           `json:"pendentes"`
	TotalConfirmado dominio.Valor `json:"total_confirmado"`
}

// ImportarLegado carrega um JSON do protótipo.
//
// Idempotente: a chave de negócio é (numero, cnpj_cliente), então reimportar
// o mesmo arquivo atualiza os pendentes e não duplica nada. Cupom já
// confirmado não é sobrescrito.
//
// Decisão importante: no JSON da Maternidade nenhum cupom tem o campo
// "status". Status ausente é tratado como CONFIRMADO — é o que reproduz
// exatamente os totais do critério de aceite (R$ 7.225,77 e R$ 11.500,46).
func (s *Servico) ImportarLegado(ctx context.Context, nomeArquivo string, dados []byte) (ResumoLegado, error) {
	var arq ArquivoLegado
	if err := json.Unmarshal(dados, &arq); err != nil {
		return ResumoLegado{}, fmt.Errorf("o arquivo %q não é um JSON do protótipo: %w", nomeArquivo, err)
	}
	if len(arq.Cupons) == 0 {
		return ResumoLegado{}, fmt.Errorf("o arquivo %q não tem nenhum cupom", nomeArquivo)
	}

	_, ctxValidacao, err := s.contexto(ctx)
	if err != nil {
		return ResumoLegado{}, err
	}

	// Slices inicializados: nil viraria null no JSON e o frontend (ou um
	// script) quebraria ao iterar.
	resumo := ResumoLegado{
		Arquivo:     nomeArquivo,
		Total:       len(arq.Cupons),
		Erros:       []string{},
		Conferencia: []ConferenciaCliente{},
	}
	porCliente := map[string]*ConferenciaCliente{}

	for _, cl := range arq.Cupons {
		cupom, avisos := converterLegado(cl)

		// Guarda a foto, se o JSON a trouxer embutida.
		if cl.Foto != "" {
			if fotoID, err := s.guardarFotoLegado(ctx, cl.Foto); err != nil {
				avisos = append(avisos, fmt.Sprintf("Não foi possível guardar a foto: %v", err))
			} else {
				cupom.FotoID = fotoID
			}
		}

		// Valida com as regras atuais, mas PRESERVA o status do legado: os
		// dados de referência já passaram por conferência humana e seus
		// totais são o critério de aceite. A validação aqui serve para
		// registrar avisos, não para rebaixar o cupom.
		statusLegado := cupom.Status
		validacao.Valida(&cupom, ctxValidacao)
		cupom.Status = statusLegado
		cupom.Avisa(avisos...)

		grav, err := s.store.SalvarCupom(ctx, cupom, nil)
		if err != nil {
			resumo.Erros = append(resumo.Erros,
				fmt.Sprintf("cupom %s (%s): %v", cl.Num, cl.ID, err))
			continue
		}

		switch {
		case grav.Ignorado:
			resumo.Ignorados++
		case grav.Criado:
			resumo.Criados++
		case grav.Atualizado:
			resumo.Atualizados++
		}

		// Conferência por cliente.
		chave := cupom.CNPJCliente
		conf, ok := porCliente[chave]
		if !ok {
			conf = &ConferenciaCliente{Cliente: cupom.Cliente, CNPJ: chave}
			porCliente[chave] = conf
		}
		if cupom.Status == dominio.StatusConfirmado {
			conf.Confirmados++
			conf.TotalConfirmado += cupom.Total
		} else {
			conf.Pendentes++
		}
	}

	for _, c := range porCliente {
		resumo.Conferencia = append(resumo.Conferencia, *c)
	}
	return resumo, nil
}

// converterLegado traduz um cupom do protótipo para o domínio.
func converterLegado(cl CupomLegado) (dominio.Cupom, []string) {
	var avisos []string

	c := dominio.Cupom{
		Numero:       strings.TrimSpace(cl.Num),
		Cliente:      strings.TrimSpace(cl.Cli),
		CNPJCliente:  dominio.SoDigitos(cl.CNPJCli),
		CNPJEmitente: dominio.SoDigitos(cl.CNPJEmp),
		Total:        dominio.ValorDeReais(cl.Total),
		Fonte:        strings.TrimSpace(cl.Fonte),
		Obs:          strings.TrimSpace(cl.Obs),
		Avisos:       append([]string(nil), cl.Avisos...),
	}
	if c.CNPJEmitente == "" {
		c.CNPJEmitente = dominio.CNPJEmitente
	}

	if cl.Data != "" {
		if d, err := dominio.ParseData(cl.Data); err == nil {
			c.Data = d
		} else {
			avisos = append(avisos, fmt.Sprintf("Data %q do legado não foi reconhecida.", cl.Data))
		}
	}

	// Status ausente no JSON = cupom já conferido no protótipo.
	switch strings.TrimSpace(cl.Status) {
	case "", string(dominio.StatusConfirmado):
		c.Status = dominio.StatusConfirmado
	case string(dominio.StatusRejeitado):
		c.Status = dominio.StatusRejeitado
	default:
		c.Status = dominio.StatusPendente
	}

	for _, campo := range cl.Inc {
		switch strings.TrimSpace(campo) {
		case "num", "numero":
			c.MarcaIncerto(dominio.CampoNumero)
		case "data":
			c.MarcaIncerto(dominio.CampoData)
		case "total":
			c.MarcaIncerto(dominio.CampoTotal)
		case "cnpjCli":
			c.MarcaIncerto(dominio.CampoCNPJCli)
		case "cli":
			c.MarcaIncerto(dominio.CampoCliente)
		}
	}

	for i, il := range cl.Itens {
		it := dominio.Item{
			Ordem:   i + 1,
			Codigo:  strings.TrimSpace(il.Cod),
			Produto: strings.TrimSpace(il.Prod),
			Qtd:     dominio.QtdDeUnidades(il.Qtd),
			Vun:     dominio.ValorDeReais(il.Vun),
			Vtot:    dominio.ValorDeReais(il.Vtot),
		}
		switch strings.ToUpper(strings.TrimSpace(il.Un)) {
		case "KG":
			it.Unidade = dominio.KG
		default:
			it.Unidade = dominio.UND
		}
		if it.Produto == "" && il.Desc != "" {
			it.Produto = strings.TrimSpace(il.Desc)
		}
		for _, campo := range il.Inc {
			switch strings.TrimSpace(campo) {
			case "cod":
				it.MarcaIncerto(dominio.CampoCod)
			case "prod":
				it.MarcaIncerto(dominio.CampoProd)
			case "qtd":
				it.MarcaIncerto(dominio.CampoQtd)
			case "vun":
				it.MarcaIncerto(dominio.CampoVun)
			case "vtot":
				it.MarcaIncerto(dominio.CampoVtot)
			}
		}
		c.Itens = append(c.Itens, it)
	}

	return c, avisos
}

// guardarFotoLegado decodifica a data URL do JSON e grava a imagem.
func (s *Servico) guardarFotoLegado(ctx context.Context, dataURL string) (*int64, error) {
	i := strings.Index(dataURL, ",")
	if !strings.HasPrefix(dataURL, "data:") || i < 0 {
		return nil, fmt.Errorf("a foto não está em formato data URL")
	}
	bruto, err := base64.StdEncoding.DecodeString(dataURL[i+1:])
	if err != nil {
		return nil, fmt.Errorf("base64 inválido: %w", err)
	}

	mime := imagem.DetectarMime(bruto)
	if !imagem.MimeSuportado(mime) {
		return nil, fmt.Errorf("tipo de imagem não suportado: %s", mime)
	}

	sha := store.SHA256Hex(bruto)
	if f, err := s.store.FotoPorSHA(ctx, sha); err == nil {
		return &f.ID, nil // já importada: reaproveita
	} else if !ehNaoEncontrado(err) {
		return nil, err
	}

	largura, altura := 0, 0
	if img, err := imagem.Normalizar(bruto, 0); err == nil {
		largura, altura = img.Largura, img.Altura
	}

	foto, err := s.store.InserirFoto(ctx, store.Foto{
		SHA256: sha, Mime: mime, Conteudo: bruto, Largura: largura, Altura: altura,
	})
	if err != nil {
		return nil, err
	}
	return &foto.ID, nil
}
