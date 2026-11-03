package pdf

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// jpegDeTeste gera um JPEG real, para que a checagem de assinatura e o
// tamanho do stream sejam exercitados de verdade.
func jpegDeTeste(t *testing.T, largura, altura int, tom uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, largura, altura))
	for y := 0; y < altura; y++ {
		for x := 0; x < largura; x++ {
			img.SetGray(x, y, color.Gray{Y: tom})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pdfComJPEGs monta um PDF mínimo, uma página por JPEG, no formato que o
// escâner do iPhone produz (XObject de imagem com filtro DCTDecode).
func pdfComJPEGs(t *testing.T, imagens [][]byte, declararLength bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	// Objetos: 1 = Catalog, 2 = Pages, depois (Page, Image) por imagem.
	primeiraPagina := 3
	var kids string
	for i := range imagens {
		kids += fmt.Sprintf("%d 0 R ", primeiraPagina+i*2)
	}

	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	buf.WriteString(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Count %d /Kids [ %s] >>\nendobj\n",
		len(imagens), kids))

	for i, img := range imagens {
		objPagina := primeiraPagina + i*2
		objImagem := objPagina + 1

		buf.WriteString(fmt.Sprintf(
			"%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
				"/Resources << /XObject << /Im0 %d 0 R >> >> >>\nendobj\n",
			objPagina, objImagem))

		length := "/Length " + fmt.Sprint(len(img))
		if !declararLength {
			// Alguns PDFs usam referência indireta; o extrator precisa achar
			// o fim do stream pelo "endstream".
			length = "/Length 999 0 R"
		}
		buf.WriteString(fmt.Sprintf(
			"%d 0 obj\n<< /Type /XObject /Subtype /Image /Width 10 /Height 10 "+
				"/ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode %s >>\nstream\n",
			objImagem, length))
		buf.Write(img)
		buf.WriteString("\nendstream\nendobj\n")
	}

	buf.WriteString("trailer\n<< /Root 1 0 R >>\n%%EOF\n")
	return buf.Bytes()
}

func TestEhPDF(t *testing.T) {
	if !EhPDF([]byte("%PDF-1.7\n...")) {
		t.Error("deveria reconhecer o cabeçalho PDF")
	}
	if EhPDF(jpegDeTeste(t, 4, 4, 200)) {
		t.Error("JPEG não é PDF")
	}
}

func TestContarPaginas(t *testing.T) {
	doc := pdfComJPEGs(t, [][]byte{
		jpegDeTeste(t, 10, 10, 10),
		jpegDeTeste(t, 10, 10, 20),
		jpegDeTeste(t, 10, 10, 30),
	}, true)
	if n := ContarPaginas(doc); n != 3 {
		t.Errorf("ContarPaginas = %d, quer 3", n)
	}
}

func TestPaginasExtraiJPEGSemRecompressao(t *testing.T) {
	originais := [][]byte{
		jpegDeTeste(t, 12, 20, 10),
		jpegDeTeste(t, 12, 20, 128),
		jpegDeTeste(t, 12, 20, 240),
	}
	doc := pdfComJPEGs(t, originais, true)

	paginas, err := NovoExtrator().Paginas(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(paginas) != len(originais) {
		t.Fatalf("extraiu %d páginas, quer %d", len(paginas), len(originais))
	}
	for i, p := range paginas {
		if p.Numero != i+1 {
			t.Errorf("página %d numerada como %d", i, p.Numero)
		}
		if p.Mime != "image/jpeg" {
			t.Errorf("página %d mime = %q", p.Numero, p.Mime)
		}
		if p.Origem != "jpeg-embutido" {
			t.Errorf("página %d origem = %q, quer jpeg-embutido", p.Numero, p.Origem)
		}
		// Byte a byte: recompressão apagaria traço fino de impressão térmica.
		if !bytes.Equal(p.Dados, originais[i]) {
			t.Errorf("página %d foi alterada (%d bytes extraídos, %d originais)",
				p.Numero, len(p.Dados), len(originais[i]))
		}
		if _, err := jpeg.Decode(bytes.NewReader(p.Dados)); err != nil {
			t.Errorf("página %d não decodifica como JPEG: %v", p.Numero, err)
		}
	}
}

func TestPaginasComLengthIndireto(t *testing.T) {
	// Sem /Length utilizável, o fim do stream vem do "endstream".
	originais := [][]byte{jpegDeTeste(t, 12, 20, 60), jpegDeTeste(t, 12, 20, 180)}
	doc := pdfComJPEGs(t, originais, false)

	paginas, err := NovoExtrator().Paginas(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(paginas) != 2 {
		t.Fatalf("extraiu %d páginas, quer 2", len(paginas))
	}
	for i, p := range paginas {
		if !bytes.Equal(p.Dados, originais[i]) {
			t.Errorf("página %d: %d bytes, quer %d", p.Numero, len(p.Dados), len(originais[i]))
		}
	}
}

func TestPaginasRejeitaNaoPDF(t *testing.T) {
	if _, err := NovoExtrator().Paginas(context.Background(), []byte("qualquer coisa")); err == nil {
		t.Error("deveria recusar um arquivo que não é PDF")
	}
}

func TestEhImagemJPEG(t *testing.T) {
	casos := []struct {
		nome string
		dict string
		quer bool
	}{
		{"imagem DCTDecode", "<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 10 >>", true},
		{"filtro em array único", "<< /Subtype /Image /Filter [ /DCTDecode ] /Length 10 >>", true},
		{"filtro em cadeia não serve", "<< /Subtype /Image /Filter [ /FlateDecode /DCTDecode ] >>", false},
		{"PNG embutido", "<< /Subtype /Image /Filter /FlateDecode >>", false},
		{"conteúdo de página", "<< /Length 42 >>", false},
		{"fonte", "<< /Type /Font /Subtype /Type1 >>", false},
	}
	for _, c := range casos {
		if got := ehImagemJPEG(c.dict); got != c.quer {
			t.Errorf("%s: ehImagemJPEG = %v, quer %v", c.nome, got, c.quer)
		}
	}
}
