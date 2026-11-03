// Package imagem prepara imagens de cupom para a API de visão do Claude.
//
// A API tem dois limites duros: 5 MB por imagem e 8000 px por lado. Acima de
// ~1568 px no lado maior a própria API reduz a imagem antes de processar, então
// mandar mais do que isso só gasta banda. Para cupom térmico apagado, porém,
// detalhe é tudo — o lado máximo é configurável e o reprocessamento manda em
// resolução cheia.
package imagem

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // registra o decodificador de PNG
	"net/http"
	"strings"

	xdraw "golang.org/x/image/draw"
)

const (
	// BytesMax é o teto por imagem aceito pela API (5 MB), com folga.
	BytesMax = 4_500_000
	// LadoMaxAbsoluto é o teto de pixels por lado aceito pela API.
	LadoMaxAbsoluto = 8000
	// LadoOtimo é o lado maior a partir do qual a API já reduziria sozinha.
	LadoOtimo = 1568
)

// Imagem é uma imagem pronta para ir à API.
type Imagem struct {
	Mime            string
	Dados           []byte
	Largura, Altura int
}

// MimeSuportado informa se o tipo é aceito pela API de visão.
func MimeSuportado(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	return false
}

// DetectarMime identifica o tipo pelos bytes iniciais.
func DetectarMime(dados []byte) string {
	mime := http.DetectContentType(dados)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	return strings.TrimSpace(mime)
}

// Normalizar ajusta a imagem aos limites da API.
//
// ladoMax é o lado maior desejado; 0 significa "não redimensionar, só
// respeitar os limites duros". GIF e WebP não são reprocessados (não há
// codificador na biblioteca padrão): se estourarem o limite, viram erro, que
// é melhor do que enviar algo que a API vai recusar.
func Normalizar(dados []byte, ladoMax int) (Imagem, error) {
	mime := DetectarMime(dados)
	if !MimeSuportado(mime) {
		return Imagem{}, fmt.Errorf("tipo de imagem não suportado pela leitura: %s", mime)
	}

	if mime == "image/gif" || mime == "image/webp" {
		if len(dados) > BytesMax {
			return Imagem{}, fmt.Errorf("imagem %s tem %.1f MB, acima do limite de %.1f MB da API",
				mime, float64(len(dados))/1e6, float64(BytesMax)/1e6)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(dados))
		if err != nil {
			return Imagem{Mime: mime, Dados: dados}, nil
		}
		return Imagem{Mime: mime, Dados: dados, Largura: cfg.Width, Altura: cfg.Height}, nil
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(dados))
	if err != nil {
		return Imagem{}, fmt.Errorf("imagem ilegível: %w", err)
	}

	limite := ladoMax
	if limite <= 0 || limite > LadoMaxAbsoluto {
		limite = LadoMaxAbsoluto
	}
	maiorLado := max(cfg.Width, cfg.Height)

	// Já está dentro de tudo: devolve os bytes originais, sem recompressão.
	if maiorLado <= limite && len(dados) <= BytesMax {
		return Imagem{Mime: mime, Dados: dados, Largura: cfg.Width, Altura: cfg.Height}, nil
	}

	img, _, err := image.Decode(bytes.NewReader(dados))
	if err != nil {
		return Imagem{}, fmt.Errorf("imagem ilegível: %w", err)
	}
	if maiorLado > limite {
		img = redimensionar(img, limite)
	}

	// Reduz a qualidade só até caber; começa alto porque traço fino de
	// impressão térmica é o primeiro a desaparecer.
	for _, q := range []int{92, 85, 75, 65, 55} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return Imagem{}, err
		}
		if buf.Len() <= BytesMax {
			b := img.Bounds()
			return Imagem{Mime: "image/jpeg", Dados: buf.Bytes(), Largura: b.Dx(), Altura: b.Dy()}, nil
		}
	}
	return Imagem{}, fmt.Errorf("não foi possível reduzir a imagem para abaixo de %.1f MB", float64(BytesMax)/1e6)
}

// redimensionar reduz a imagem para que o lado maior fique em ladoMax,
// preservando a proporção. Usa Catmull-Rom: reamostragem mais simples
// destrói o traço fino do cupom.
func redimensionar(src image.Image, ladoMax int) image.Image {
	b := src.Bounds()
	largura, altura := b.Dx(), b.Dy()
	if largura <= 0 || altura <= 0 {
		return src
	}
	var nl, na int
	if largura >= altura {
		nl = ladoMax
		na = altura * ladoMax / largura
	} else {
		na = ladoMax
		nl = largura * ladoMax / altura
	}
	nl, na = max(nl, 1), max(na, 1)

	dst := image.NewRGBA(image.Rect(0, 0, nl, na))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}
