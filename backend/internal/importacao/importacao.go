// Package importacao orquestra a entrada de cupons no sistema: recebe PDFs e
// imagens, fatia em páginas, manda cada página para a leitura via Claude,
// aplica as validações e grava tudo como pendente de revisão.
package importacao

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/imagem"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/leitura"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/pdf"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/validacao"
)

// Arquivo é um upload recebido.
type Arquivo struct {
	Nome  string
	Dados []byte
}

// Servico executa as importações.
type Servico struct {
	store    Store
	leitor   leitura.Leitor
	extrator *pdf.Extrator
	log      *slog.Logger

	// Concorrencia limita quantas páginas vão à API ao mesmo tempo.
	Concorrencia int
	// LadoMaxImagem é o lado maior enviado à API; 0 manda sem redimensionar.
	LadoMaxImagem int
	// LimiteDiario corta o lote se o consumo do dia já estourou; 0 desliga.
	LimiteDiario int
	// MesReferencia é passado às validações para detectar ano/mês suspeito.
	MesReferencia string
}

// Store é o que a importação precisa do banco. A interface mantém o serviço
// testável sem Postgres.
type Store interface {
	FotoPorSHA(ctx context.Context, sha string) (store.Foto, error)
	InserirFoto(ctx context.Context, f store.Foto) (store.Foto, error)
	ConteudoFoto(ctx context.Context, id int64) (mime string, dados []byte, err error)
	CriarImportacao(ctx context.Context, arquivos []string, totalPaginas int, usuarioID *int64) (int64, error)
	FinalizarImportacao(ctx context.Context, imp store.Importacao) error
	SalvarCupom(ctx context.Context, c dominio.Cupom, importacaoID *int64) (store.ResultadoGravacao, error)
	RegistrarLeitura(ctx context.Context, r store.RegistroLeitura) error
	LeiturasHoje(ctx context.Context) (int, error)
	CarregarCatalogo(ctx context.Context) (*store.CatalogoMemoria, error)
	ListarClientes(ctx context.Context, incluirInativos bool) ([]dominio.Cliente, error)
	GarantirProduto(ctx context.Context, codigo, descricao string, un dominio.Unidade) error
}

// NovoServico monta o serviço com os padrões de produção.
func NovoServico(st Store, leitor leitura.Leitor, log *slog.Logger) *Servico {
	return &Servico{
		store:         st,
		leitor:        leitor,
		extrator:      pdf.NovoExtrator(),
		log:           log,
		Concorrencia:  3,
		LadoMaxImagem: imagem.LadoOtimo,
	}
}

// pagina é uma página pronta para leitura.
type pagina struct {
	arquivo string
	numero  int
	// fonte é o rótulo humano gravado no cupom ("cupom.pdf p.7").
	fonte string
	img   imagem.Imagem
	sha   string
	// erro preenchido quando a página nem chegou a ser lida.
	erro error
}

// Importar processa o lote inteiro e devolve o resumo.
func (s *Servico) Importar(ctx context.Context, arquivos []Arquivo, usuarioID *int64) (store.Importacao, error) {
	if len(arquivos) == 0 {
		return store.Importacao{}, fmt.Errorf("nenhum arquivo foi enviado")
	}

	// 1. Fatia tudo em páginas antes de chamar a API: se um PDF estiver
	//    corrompido, o operador descobre sem gastar leitura.
	var paginas []pagina
	nomes := make([]string, 0, len(arquivos))
	for _, a := range arquivos {
		nomes = append(nomes, a.Nome)
		paginas = append(paginas, s.fatiar(ctx, a)...)
	}

	impID, err := s.store.CriarImportacao(ctx, nomes, len(paginas), usuarioID)
	if err != nil {
		return store.Importacao{}, err
	}

	s.log.Info("importação iniciada",
		"importacao", impID, "arquivos", len(arquivos), "paginas", len(paginas),
		"mes_referencia", s.MesReferencia, "concorrencia", s.Concorrencia)
	imp := store.Importacao{ID: impID, Arquivos: nomes, TotalPaginas: len(paginas), Status: "concluida"}

	// 2. Limite diário de custo: barra o lote antes de começar.
	if s.LimiteDiario > 0 {
		usadas, err := s.store.LeiturasHoje(ctx)
		if err != nil {
			s.log.Warn("não foi possível conferir o limite diário de leituras", "erro", err)
		} else if usadas+len(paginas) > s.LimiteDiario {
			imp.Status = "falhou"
			imp.Detalhe = []store.DetalhePagina{{
				Situacao: "erro",
				Erro: fmt.Sprintf("o limite diário de %d leituras seria estourado (%d já usadas, %d páginas neste lote). "+
					"Ajuste LIMITE_LEITURAS_DIA ou importe amanhã.", s.LimiteDiario, usadas, len(paginas)),
			}}
			imp.Erros = 1
			s.log.Error("importação barrada pelo limite diário de leituras",
				"importacao", impID, "limite", s.LimiteDiario, "usadas_hoje", usadas,
				"paginas_do_lote", len(paginas))
			if err := s.store.FinalizarImportacao(ctx, imp); err != nil {
				return imp, err
			}
			return imp, nil
		}
	}

	// 3. Carrega catálogo e clientes uma vez: o prompt é idêntico em todas as
	//    páginas, o que mantém o cache de prompt válido no lote inteiro.
	ref, ctxValidacao, err := s.contexto(ctx)
	if err != nil {
		return store.Importacao{}, err
	}

	// 4. Lê as páginas com concorrência limitada.
	detalhes := make([]store.DetalhePagina, len(paginas))
	grupo, ctxGrupo := errgroup.WithContext(ctx)
	grupo.SetLimit(max(s.Concorrencia, 1))

	var mu sync.Mutex
	for i := range paginas {
		i := i
		grupo.Go(func() error {
			d := s.processar(ctxGrupo, paginas[i], impID, ref, ctxValidacao)
			mu.Lock()
			detalhes[i] = d
			mu.Unlock()
			return nil // uma página com erro não derruba o lote
		})
	}
	if err := grupo.Wait(); err != nil {
		return store.Importacao{}, err
	}

	// 5. Consolida.
	imp.Detalhe = detalhes
	for _, d := range detalhes {
		switch d.Situacao {
		case "lido":
			imp.Lidas++
			if len(d.Avisos) > 0 {
				imp.ComPendencia++
			}
		case "duplicado":
			imp.Duplicadas++
		case "erro":
			imp.Erros++
		}
	}
	if imp.Lidas == 0 && imp.Erros > 0 {
		imp.Status = "falhou"
	}

	nivel := slog.LevelInfo
	if imp.Erros > 0 {
		nivel = slog.LevelError
	}
	s.log.Log(ctx, nivel, "importação concluída",
		"importacao", impID, "status", imp.Status, "paginas", imp.TotalPaginas,
		"lidas", imp.Lidas, "com_pendencia", imp.ComPendencia,
		"duplicadas", imp.Duplicadas, "erros", imp.Erros)

	if err := s.store.FinalizarImportacao(ctx, imp); err != nil {
		return imp, err
	}
	return imp, nil
}

// contexto monta as referências do prompt e o contexto de validação.
func (s *Servico) contexto(ctx context.Context) (leitura.Referencias, validacao.Contexto, error) {
	catalogo, err := s.store.CarregarCatalogo(ctx)
	if err != nil {
		return leitura.Referencias{}, validacao.Contexto{}, err
	}
	clientes, err := s.store.ListarClientes(ctx, false)
	if err != nil {
		return leitura.Referencias{}, validacao.Contexto{}, err
	}

	cnpjs := make([]string, 0, len(clientes))
	for _, c := range clientes {
		cnpjs = append(cnpjs, c.CNPJ)
	}

	return leitura.Referencias{Produtos: catalogo.Todos(), Clientes: clientes},
		validacao.Contexto{Catalogo: catalogo, CNPJConhecidos: cnpjs, MesReferencia: s.MesReferencia},
		nil
}

// fatiar transforma um arquivo em páginas-imagem normalizadas.
func (s *Servico) fatiar(ctx context.Context, a Arquivo) []pagina {
	if pdf.EhPDF(a.Dados) {
		pags, err := s.extrator.Paginas(ctx, a.Dados)
		if err != nil {
			return []pagina{{arquivo: a.Nome, fonte: a.Nome, erro: err}}
		}
		out := make([]pagina, 0, len(pags))
		for _, p := range pags {
			fonte := fmt.Sprintf("%s p.%d", a.Nome, p.Numero)
			img, err := imagem.Normalizar(p.Dados, s.LadoMaxImagem)
			if err != nil {
				out = append(out, pagina{arquivo: a.Nome, numero: p.Numero, fonte: fonte, erro: err})
				continue
			}
			out = append(out, pagina{
				arquivo: a.Nome, numero: p.Numero, fonte: fonte,
				img: img,
				sha: store.SHA256Hex(img.Dados),
			})
		}
		return out
	}

	// Imagem solta = um cupom. Foto alta de celular NÃO é fatiada em vários
	// cupons (CLAUDE.md §5.2).
	img, err := imagem.Normalizar(a.Dados, s.LadoMaxImagem)
	if err != nil {
		return []pagina{{arquivo: a.Nome, numero: 1, fonte: a.Nome, erro: err}}
	}
	return []pagina{{
		arquivo: a.Nome, numero: 1, fonte: a.Nome,
		img: img, sha: store.SHA256Hex(img.Dados),
	}}
}

// processar lê uma página e grava o cupom resultante.
func (s *Servico) processar(ctx context.Context, p pagina, impID int64,
	ref leitura.Referencias, ctxValidacao validacao.Contexto) store.DetalhePagina {

	d := store.DetalhePagina{Arquivo: p.arquivo, Pagina: p.numero}

	if p.erro != nil {
		s.log.Error("pagina nao pôde ser preparada para leitura",
			"arquivo", p.arquivo, "pagina", p.numero, "erro", p.erro)
		d.Situacao = "erro"
		d.Erro = p.erro.Error()
		return d
	}

	// Mesma imagem já importada: não gasta leitura nem duplica cupom.
	if foto, err := s.store.FotoPorSHA(ctx, p.sha); err == nil {
		d.Situacao = "duplicado"
		d.Erro = fmt.Sprintf("esta imagem já foi importada antes (foto %d)", foto.ID)
		return d
	} else if !ehNaoEncontrado(err) {
		s.log.Error("falha ao conferir imagens já importadas",
			"arquivo", p.arquivo, "pagina", p.numero, "erro", err)
		d.Situacao = "erro"
		d.Erro = fmt.Sprintf("falha ao conferir imagens já importadas: %v", err)
		return d
	}

	foto, err := s.store.InserirFoto(ctx, store.Foto{
		SHA256: p.sha, Mime: p.img.Mime, Conteudo: p.img.Dados,
		Largura: p.img.Largura, Altura: p.img.Altura,
	})
	if err != nil {
		s.log.Error("falha ao guardar a imagem do cupom",
			"arquivo", p.arquivo, "pagina", p.numero, "erro", err)
		d.Situacao = "erro"
		d.Erro = fmt.Sprintf("falha ao guardar a imagem: %v", err)
		return d
	}

	res, errLeitura := s.leitor.Ler(ctx, p.img, p.fonte, ref)

	// O log de custo é gravado mesmo quando a leitura falha: a chamada foi
	// cobrada de todo jeito.
	registro := store.RegistroLeitura{
		FotoID: &foto.ID, ImportacaoID: &impID,
		Modelo: res.Modelo, TokensEntrada: res.TokensEntrada, TokensSaida: res.TokensSaida,
		Duracao: res.Duracao, RespostaBruta: res.Bruto,
	}
	if errLeitura != nil {
		registro.Erro = errLeitura.Error()
	}
	if err := s.store.RegistrarLeitura(ctx, registro); err != nil {
		s.log.Warn("não foi possível registrar a leitura no log", "erro", err, "fonte", p.fonte)
	}

	if errLeitura != nil {
		// Antes isto ia SÓ para leituras_log, no banco. Quem olha o log do
		// contêiner não via nada, e uma importação que falhou inteira parecia
		// não ter acontecido.
		s.log.Error("falha ao ler cupom",
			"arquivo", p.arquivo, "pagina", p.numero, "fonte", p.fonte,
			"modelo", res.Modelo, "foto_id", foto.ID, "erro", errLeitura)
		d.Situacao = "erro"
		d.Erro = errLeitura.Error()
		return d
	}

	cupom := res.Cupom
	cupom.FotoID = &foto.ID
	cupom.Status = dominio.StatusPendente // toda leitura nasce pendente
	validacao.Valida(&cupom, ctxValidacao)

	grav, err := s.store.SalvarCupom(ctx, cupom, &impID)
	if err != nil {
		s.log.Error("falha ao gravar o cupom lido",
			"arquivo", p.arquivo, "pagina", p.numero, "numero", cupom.Numero, "erro", err)
		d.Situacao = "erro"
		d.Erro = fmt.Sprintf("falha ao gravar o cupom: %v", err)
		return d
	}

	if grav.Ignorado {
		d.Situacao = "duplicado"
		d.Numero = cupom.Numero
		d.Cliente = cupom.Cliente
		d.Erro = fmt.Sprintf("o cupom nº %s deste cliente já está confirmado e não foi sobrescrito",
			cupom.Numero)
		return d
	}

	// Cadastra produtos novos como "a confirmar", para aparecerem em Cadastros.
	for _, it := range cupom.Itens {
		if it.Codigo == "" {
			continue
		}
		if _, ok := ctxValidacao.Catalogo.PorCodigo(it.Codigo); ok {
			continue
		}
		if err := s.store.GarantirProduto(ctx, it.Codigo, it.Produto, it.Unidade); err != nil {
			s.log.Warn("não foi possível cadastrar produto novo", "codigo", it.Codigo, "erro", err)
		}
	}

	total := grav.Cupom.Total
	d.Situacao = "lido"
	d.CupomID = grav.Cupom.ID
	d.Numero = grav.Cupom.Numero
	d.Cliente = grav.Cupom.Cliente
	d.Total = &total
	d.Avisos = grav.Cupom.Avisos
	return d
}

func ehNaoEncontrado(err error) bool {
	return errors.Is(err, store.ErroNaoEncontrado)
}

// ImportarComMes é o Importar com o mês de referência do lote, usado para
// detectar ano/mês lido errado.
func (s *Servico) ImportarComMes(ctx context.Context, arquivos []Arquivo, usuarioID *int64, mes string) (store.Importacao, error) {
	if mes == "" {
		return s.Importar(ctx, arquivos, usuarioID)
	}
	// Cópia rasa: o mês vale só para este lote, sem afetar chamadas paralelas.
	copia := *s
	copia.MesReferencia = mes
	return copia.Importar(ctx, arquivos, usuarioID)
}

// Reprocessar relê a foto já guardada de um cupom, opcionalmente com outro
// modelo, e regrava o resultado.
//
// A foto não é reenviada nem reprocessada: é a mesma imagem, relida. O cupom
// volta a pendente, porque a leitura mudou e precisa de olho humano de novo.
func (s *Servico) Reprocessar(ctx context.Context, atual dominio.Cupom, modelo string) (dominio.Cupom, error) {
	if atual.FotoID == nil {
		return dominio.Cupom{}, fmt.Errorf("este cupom não tem foto guardada; não há o que reler")
	}

	mime, dados, err := s.store.ConteudoFoto(ctx, *atual.FotoID)
	if err != nil {
		return dominio.Cupom{}, fmt.Errorf("não foi possível recuperar a foto: %w", err)
	}

	// No reprocessamento vale gastar resolução: é um cupom difícil, escolhido
	// a dedo pelo operador.
	img, err := imagem.Normalizar(dados, 0)
	if err != nil {
		img = imagem.Imagem{Mime: mime, Dados: dados}
	}

	ref, ctxValidacao, err := s.contexto(ctx)
	if err != nil {
		return dominio.Cupom{}, err
	}

	leitor := s.leitor
	if modelo != "" {
		if trocavel, ok := s.leitor.(interface {
			ComModelo(string) *leitura.ClienteClaude
		}); ok {
			leitor = trocavel.ComModelo(modelo)
		}
	}

	res, errLeitura := leitor.Ler(ctx, img, atual.Fonte, ref)

	registro := store.RegistroLeitura{
		FotoID: atual.FotoID, CupomID: &atual.ID,
		Modelo: res.Modelo, TokensEntrada: res.TokensEntrada, TokensSaida: res.TokensSaida,
		Duracao: res.Duracao, RespostaBruta: res.Bruto,
	}
	if errLeitura != nil {
		registro.Erro = errLeitura.Error()
	}
	if err := s.store.RegistrarLeitura(ctx, registro); err != nil {
		s.log.Warn("não foi possível registrar o reprocessamento", "cupom", atual.ID, "erro", err)
	}
	if errLeitura != nil {
		return dominio.Cupom{}, errLeitura
	}

	novo := res.Cupom
	novo.ID = atual.ID
	novo.FotoID = atual.FotoID
	novo.Status = dominio.StatusPendente
	novo.Fonte = atual.Fonte
	// A observação do operador é trabalho humano: não se perde numa releitura.
	novo.Obs = atual.Obs
	if novo.Numero == "" {
		novo.Numero = atual.Numero
	}

	validacao.Valida(&novo, ctxValidacao)
	novo.Avisa(fmt.Sprintf("Cupom relido com o modelo %s.", res.Modelo))

	grav, err := s.store.SalvarCupom(ctx, novo, nil)
	if err != nil {
		return dominio.Cupom{}, err
	}
	return grav.Cupom, nil
}

// UsarExtrator troca o extrator de PDF (usado para aplicar a configuração de
// pdftoppm e DPI do ambiente).
func (s *Servico) UsarExtrator(e *pdf.Extrator) {
	if e != nil {
		s.extrator = e
	}
}
