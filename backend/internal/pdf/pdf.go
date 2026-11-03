// Package pdf transforma um PDF escaneado em uma imagem por página.
//
// Duas estratégias, nessa ordem:
//
//  1. Extração direta do JPEG embutido. PDFs gerados pelo escâner do iPhone
//     (que é como os cupons chegam) contêm exatamente um JPEG por página,
//     com filtro DCTDecode. Copiar esses bytes preserva cada pixel da
//     impressão térmica — recompressão é justamente o que apaga traço fino.
//  2. Rasterização com pdftoppm (poppler-utils), para PDFs que não sejam
//     "um JPEG por página": vetoriais, PNG embutido, multi-imagem.
package pdf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Pagina é uma página do PDF já como imagem.
type Pagina struct {
	Numero int    // 1-based
	Mime   string // "image/jpeg" ou "image/png"
	Dados  []byte
	// Origem descreve como a imagem foi obtida, para auditoria.
	Origem string // "jpeg-embutido" ou "pdftoppm"
}

// Extrator converte PDFs em páginas-imagem.
type Extrator struct {
	// Pdftoppm é o caminho do binário; vazio usa "pdftoppm" do PATH.
	Pdftoppm string
	// DPI usado na rasterização de fallback.
	DPI int
}

// NovoExtrator devolve um extrator com os padrões de produção.
func NovoExtrator() *Extrator { return &Extrator{Pdftoppm: "pdftoppm", DPI: 300} }

// EhPDF identifica um PDF pelo cabeçalho.
func EhPDF(dados []byte) bool { return bytes.HasPrefix(dados, []byte("%PDF-")) }

// Paginas devolve uma imagem por página do PDF.
func (e *Extrator) Paginas(ctx context.Context, dados []byte) ([]Pagina, error) {
	if !EhPDF(dados) {
		return nil, fmt.Errorf("o arquivo não é um PDF")
	}

	nPaginas := ContarPaginas(dados)

	// Estratégia 1: um JPEG embutido por página, sem recompressão.
	if jpegs := jpegsEmbutidos(dados); len(jpegs) > 0 {
		if nPaginas == 0 || len(jpegs) == nPaginas {
			return jpegs, nil
		}
		// Contagem divergente: há páginas com várias imagens (ou nenhuma).
		// Rasterizar é mais seguro do que adivinhar o mapeamento.
	}

	// Estratégia 2: rasterizar.
	paginas, err := e.rasterizar(ctx, dados)
	if err != nil {
		return nil, fmt.Errorf("não foi possível extrair as páginas do PDF: %w", err)
	}
	return paginas, nil
}

var reTipoPagina = regexp.MustCompile(`/Type\s*/Page[^s]`)

// ContarPaginas conta as páginas pelos objetos /Type /Page. Devolve 0 quando
// não consegue determinar (PDF com xref comprimido, por exemplo).
func ContarPaginas(dados []byte) int {
	return len(reTipoPagina.FindAll(dados, -1))
}

// objetoStream casa um objeto indireto com stream: "12 0 obj <<...>> stream".
var reObjeto = regexp.MustCompile(`(?s)(\d+)\s+(\d+)\s+obj\b(.*?)\bstream\r?\n`)

// jpegsEmbutidos varre os objetos do PDF e devolve os XObjects de imagem com
// filtro DCTDecode, na ordem em que aparecem no arquivo.
//
// A ordem do arquivo é assumida como ordem das páginas — válido para PDFs de
// escâner, que escrevem uma página por vez. Quando a contagem não casa com o
// número de páginas, quem chama cai para a rasterização.
func jpegsEmbutidos(dados []byte) []Pagina {
	type achado struct {
		offset int
		dados  []byte
	}
	var achados []achado

	for _, m := range reObjeto.FindAllSubmatchIndex(dados, -1) {
		dict := string(dados[m[6]:m[7]])
		if !ehImagemJPEG(dict) {
			continue
		}
		inicio := m[1] // fim de "stream\r\n"
		fim := fimDoStream(dados, inicio, comprimentoDeclarado(dict))
		if fim <= inicio {
			continue
		}
		bruto := dados[inicio:fim]
		// Confere a assinatura JPEG: SOI (FFD8FF) no começo.
		if len(bruto) < 4 || bruto[0] != 0xFF || bruto[1] != 0xD8 || bruto[2] != 0xFF {
			continue
		}
		achados = append(achados, achado{offset: inicio, dados: bruto})
	}

	sort.Slice(achados, func(i, j int) bool { return achados[i].offset < achados[j].offset })

	paginas := make([]Pagina, 0, len(achados))
	for i, a := range achados {
		// Copia: o slice aponta para o buffer do upload inteiro.
		buf := make([]byte, len(a.dados))
		copy(buf, a.dados)
		paginas = append(paginas, Pagina{
			Numero: i + 1,
			Mime:   "image/jpeg",
			Dados:  buf,
			Origem: "jpeg-embutido",
		})
	}
	return paginas
}

// ehImagemJPEG confere se o dicionário do objeto é um XObject de imagem com
// DCTDecode como único filtro. Imagem com filtro em cadeia (ex.: DCTDecode
// depois de Flate) não serve para cópia direta.
func ehImagemJPEG(dict string) bool {
	if !strings.Contains(dict, "/Subtype") || !strings.Contains(dict, "/Image") {
		return false
	}
	if !strings.Contains(dict, "/DCTDecode") {
		return false
	}
	// Filtro em array com mais de um elemento: não é cópia direta.
	if i := strings.Index(dict, "/Filter"); i >= 0 {
		resto := dict[i+len("/Filter"):]
		if j := strings.Index(resto, "["); j >= 0 && j < 8 {
			if k := strings.Index(resto, "]"); k > j {
				if strings.Count(resto[j:k], "/") > 1 {
					return false
				}
			}
		}
	}
	return true
}

var reLength = regexp.MustCompile(`/Length\s+(\d+)`)

// comprimentoDeclarado lê /Length do dicionário; 0 quando é referência
// indireta ("/Length 12 0 R"), que exigiria resolver outro objeto.
func comprimentoDeclarado(dict string) int {
	m := reLength.FindStringSubmatch(dict)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

var marcaEndstream = []byte("endstream")

// fimDoStream devolve o offset final dos dados do stream. Usa /Length quando
// ele é coerente com o "endstream" seguinte; senão procura o "endstream".
func fimDoStream(dados []byte, inicio, length int) int {
	if length > 0 && inicio+length <= len(dados) {
		fim := inicio + length
		// Confere se "endstream" vem logo depois (tolerando EOL).
		resto := dados[fim:min(fim+20, len(dados))]
		if bytes.Contains(resto, marcaEndstream) {
			return fim
		}
	}
	rel := bytes.Index(dados[inicio:], marcaEndstream)
	if rel < 0 {
		return -1
	}
	fim := inicio + rel
	// Retira o EOL que o PDF insere antes de "endstream".
	for fim > inicio && (dados[fim-1] == '\n' || dados[fim-1] == '\r') {
		fim--
	}
	return fim
}

// rasterizar converte o PDF em JPEGs com pdftoppm (poppler-utils).
func (e *Extrator) rasterizar(ctx context.Context, dados []byte) ([]Pagina, error) {
	bin := e.Pdftoppm
	if bin == "" {
		bin = "pdftoppm"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("este PDF precisa ser rasterizado, mas %s não está instalado (pacote poppler-utils)", bin)
	}

	dir, err := os.MkdirTemp("", "cupons-pdf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	entrada := filepath.Join(dir, "entrada.pdf")
	if err := os.WriteFile(entrada, dados, 0o600); err != nil {
		return nil, err
	}

	dpi := e.DPI
	if dpi <= 0 {
		dpi = 300
	}
	prefixo := filepath.Join(dir, "pag")
	cmd := exec.CommandContext(ctx, bin,
		"-jpeg", "-jpegopt", "quality=92",
		"-r", strconv.Itoa(dpi),
		entrada, prefixo,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pdftoppm falhou: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	arquivos, err := filepath.Glob(prefixo + "*.jpg")
	if err != nil {
		return nil, err
	}
	if len(arquivos) == 0 {
		return nil, fmt.Errorf("pdftoppm não gerou nenhuma página")
	}
	sort.Strings(arquivos) // pag-01.jpg, pag-02.jpg, ... ordenam corretamente

	paginas := make([]Pagina, 0, len(arquivos))
	for i, a := range arquivos {
		b, err := os.ReadFile(a)
		if err != nil {
			return nil, err
		}
		paginas = append(paginas, Pagina{
			Numero: i + 1,
			Mime:   "image/jpeg",
			Dados:  b,
			Origem: "pdftoppm",
		})
	}
	return paginas, nil
}
