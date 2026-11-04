package importacao

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/dominio"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/imagem"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/leitura"
	"github.com/ClaudioSBezerra/FB_CUPONS/backend/internal/store"
)

// storeFake é um Store em memória, para exercitar o pipeline sem Postgres.
type storeFake struct {
	fotosPorSHA map[string]store.Foto
	proxFotoID  int64
	proxCupomID int64

	cuponsSalvos []dominio.Cupom
	// confirmados simula cupons já conferidos: SalvarCupom não os sobrescreve.
	confirmados  map[string]bool
	leituras     []store.RegistroLeitura
	produtos     []dominio.Produto
	clientes     []dominio.Cliente
	garantidos   []string
	leiturasHoje int
	importacoes  []store.Importacao
}

func novoStoreFake() *storeFake {
	return &storeFake{
		fotosPorSHA: map[string]store.Foto{},
		confirmados: map[string]bool{},
		produtos: []dominio.Produto{
			{Codigo: "20771", Descricao: "PÃO FRANCÊS 50G", Unidade: dominio.UND, Confirmado: true},
			{Codigo: "22121", Descricao: "PÃO DOCE 50G", Unidade: dominio.UND, Confirmado: true},
		},
		clientes: []dominio.Cliente{
			{ID: 1, Nome: "MATERNIDADE CÉLIA CÂMARA", CNPJ: "37678845000736", Ativo: true},
			{ID: 2, Nome: "DONA ÍRIS", CNPJ: "37678845000302", Ativo: true},
		},
	}
}

func (s *storeFake) FotoPorSHA(_ context.Context, sha string) (store.Foto, error) {
	if f, ok := s.fotosPorSHA[sha]; ok {
		return f, nil
	}
	return store.Foto{}, store.ErroNaoEncontrado
}

func (s *storeFake) InserirFoto(_ context.Context, f store.Foto) (store.Foto, error) {
	if existente, ok := s.fotosPorSHA[f.SHA256]; ok {
		return existente, nil
	}
	s.proxFotoID++
	f.ID = s.proxFotoID
	s.fotosPorSHA[f.SHA256] = f
	return f, nil
}

func (s *storeFake) ConteudoFoto(_ context.Context, id int64) (string, []byte, error) {
	for _, f := range s.fotosPorSHA {
		if f.ID == id {
			return f.Mime, f.Conteudo, nil
		}
	}
	return "", nil, store.ErroNaoEncontrado
}

func (s *storeFake) CriarImportacao(_ context.Context, arquivos []string, total int, _ *int64) (int64, error) {
	s.importacoes = append(s.importacoes, store.Importacao{ID: int64(len(s.importacoes) + 1),
		Arquivos: arquivos, TotalPaginas: total})
	return int64(len(s.importacoes)), nil
}

func (s *storeFake) FinalizarImportacao(_ context.Context, imp store.Importacao) error {
	for i := range s.importacoes {
		if s.importacoes[i].ID == imp.ID {
			s.importacoes[i] = imp
		}
	}
	return nil
}

func (s *storeFake) SalvarCupom(_ context.Context, c dominio.Cupom, _ *int64) (store.ResultadoGravacao, error) {
	chave := c.Numero + "|" + c.CNPJCliente
	if c.Numero != "" && s.confirmados[chave] {
		return store.ResultadoGravacao{Cupom: c, Ignorado: true}, nil
	}
	s.proxCupomID++
	c.ID = s.proxCupomID
	s.cuponsSalvos = append(s.cuponsSalvos, c)
	return store.ResultadoGravacao{Cupom: c, Criado: true}, nil
}

func (s *storeFake) RegistrarLeitura(_ context.Context, r store.RegistroLeitura) error {
	s.leituras = append(s.leituras, r)
	return nil
}

func (s *storeFake) LeiturasHoje(context.Context) (int, error) { return s.leiturasHoje, nil }

func (s *storeFake) CarregarCatalogo(context.Context) (*store.CatalogoMemoria, error) {
	return store.CatalogoDe(s.produtos), nil
}

func (s *storeFake) ListarClientes(context.Context, bool) ([]dominio.Cliente, error) {
	return s.clientes, nil
}

func (s *storeFake) GarantirProduto(_ context.Context, codigo, _ string, _ dominio.Unidade) error {
	s.garantidos = append(s.garantidos, codigo)
	return nil
}

// --- apoio ---

func jpegCupom(t *testing.T, tom uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 40, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 40; x++ {
			img.SetGray(x, y, color.Gray{Y: tom})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pdfDeTeste monta um PDF de n páginas, uma imagem JPEG por página.
func pdfDeTeste(t *testing.T, imagens [][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	buf.WriteString("2 0 obj\n<< /Type /Pages /Count " + itoa(len(imagens)) + " >>\nendobj\n")
	obj := 3
	for _, img := range imagens {
		buf.WriteString(itoa(obj) + " 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
		obj++
		buf.WriteString(itoa(obj) + " 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length " +
			itoa(len(img)) + " >>\nstream\n")
		buf.Write(img)
		buf.WriteString("\nendstream\nendobj\n")
		obj++
	}
	buf.WriteString("trailer\n<< /Root 1 0 R >>\n%%EOF\n")
	return buf.Bytes()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func logSilencioso() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func respostaCupom(numero, cnpj, cliente string, total float64, itens string) json.RawMessage {
	return json.RawMessage(`{
	  "numero": "` + numero + `",
	  "data": "2026-10-16",
	  "cnpj_cliente": "` + cnpj + `",
	  "cliente": "` + cliente + `",
	  "total": ` + ftoa(total) + `,
	  "itens": [` + itens + `],
	  "incertos": [],
	  "avisos": []
	}`)
}

func ftoa(f float64) string {
	centavos := int(f*100 + 0.5)
	return itoa(centavos/100) + "." + pad2(centavos%100)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

const itemPaoFrances = `{"cod":"20771","prod":"PÃO FRANCÊS 50G","un":"UND","qtd":120,"vun":1.00,"vtot":120.00,"incertos":[]}`
const itemPaoDoce = `{"cod":"22121","prod":"PÃO DOCE 50G","un":"UND","qtd":110,"vun":1.50,"vtot":165.00,"incertos":[]}`

// --- testes ---

func TestImportarPDFDuasPaginas(t *testing.T) {
	st := novoStoreFake()
	leitor := leitura.NovoLeitorGravado(
		respostaCupom("1340923", "37678845000736", "MATERNIDADE CÉLIA CÂMARA", 285.00,
			itemPaoFrances+","+itemPaoDoce),
		respostaCupom("1341234", "37678845000736", "MATERNIDADE CÉLIA CÂMARA", 120.00, itemPaoFrances),
	)
	svc := NovoServico(st, leitor, logSilencioso())

	doc := pdfDeTeste(t, [][]byte{jpegCupom(t, 30), jpegCupom(t, 200)})
	imp, err := svc.Importar(context.Background(), []Arquivo{{Nome: "cupons.pdf", Dados: doc}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if imp.TotalPaginas != 2 {
		t.Errorf("total de páginas = %d, quer 2", imp.TotalPaginas)
	}
	if imp.Lidas != 2 {
		t.Errorf("lidas = %d, quer 2 (detalhe: %+v)", imp.Lidas, imp.Detalhe)
	}
	if imp.Erros != 0 {
		t.Errorf("erros = %d: %+v", imp.Erros, imp.Detalhe)
	}
	if len(st.cuponsSalvos) != 2 {
		t.Fatalf("cupons salvos = %d, quer 2", len(st.cuponsSalvos))
	}

	// A fonte identifica a página de origem, para conferência humana.
	if got := st.cuponsSalvos[0].Fonte; !strings.Contains(got, "cupons.pdf p.") {
		t.Errorf("fonte = %q, quer identificar o PDF e a página", got)
	}
	// Toda leitura nasce pendente de revisão.
	for _, c := range st.cuponsSalvos {
		if c.Status != dominio.StatusPendente {
			t.Errorf("cupom %s nasceu com status %q, quer pendente", c.Numero, c.Status)
		}
		if c.FotoID == nil {
			t.Errorf("cupom %s sem foto vinculada", c.Numero)
		}
	}
	// O custo de cada chamada é registrado.
	if len(st.leituras) != 2 {
		t.Errorf("registros de leitura = %d, quer 2", len(st.leituras))
	}
}

func TestImportarImagemSoltaNaoEhFatiada(t *testing.T) {
	// Foto alta de celular é UM cupom, não vários (CLAUDE.md §5.2).
	st := novoStoreFake()
	leitor := leitura.NovoLeitorGravado(
		respostaCupom("1340923", "37678845000736", "MATERNIDADE CÉLIA CÂMARA", 120.00, itemPaoFrances))
	svc := NovoServico(st, leitor, logSilencioso())

	imp, err := svc.Importar(context.Background(),
		[]Arquivo{{Nome: "foto.jpg", Dados: jpegCupom(t, 128)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if imp.TotalPaginas != 1 || imp.Lidas != 1 {
		t.Errorf("páginas = %d, lidas = %d; quer 1 e 1", imp.TotalPaginas, imp.Lidas)
	}
}

func TestImportarMesmaImagemDuasVezesNaoDuplica(t *testing.T) {
	st := novoStoreFake()
	leitor := leitura.NovoLeitorGravado(
		respostaCupom("1340923", "37678845000736", "MATERNIDADE CÉLIA CÂMARA", 120.00, itemPaoFrances))
	svc := NovoServico(st, leitor, logSilencioso())

	foto := jpegCupom(t, 77)
	if _, err := svc.Importar(context.Background(), []Arquivo{{Nome: "a.jpg", Dados: foto}}, nil); err != nil {
		t.Fatal(err)
	}

	// Segunda tentativa com a MESMA imagem: nem gasta leitura, nem duplica.
	imp, err := svc.Importar(context.Background(), []Arquivo{{Nome: "a-copia.jpg", Dados: foto}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Duplicadas != 1 {
		t.Errorf("duplicadas = %d, quer 1 (detalhe: %+v)", imp.Duplicadas, imp.Detalhe)
	}
	if len(st.cuponsSalvos) != 1 {
		t.Errorf("cupons salvos = %d, quer 1", len(st.cuponsSalvos))
	}
	if len(st.leituras) != 1 {
		t.Errorf("leituras = %d; a segunda importação não deveria chamar a API", len(st.leituras))
	}
}

func TestImportarNaoSobrescreveCupomConfirmado(t *testing.T) {
	st := novoStoreFake()
	st.confirmados["1340923|37678845000736"] = true

	leitor := leitura.NovoLeitorGravado(
		respostaCupom("1340923", "37678845000736", "MATERNIDADE CÉLIA CÂMARA", 999.00, itemPaoFrances))
	svc := NovoServico(st, leitor, logSilencioso())

	imp, err := svc.Importar(context.Background(),
		[]Arquivo{{Nome: "reimport.jpg", Dados: jpegCupom(t, 44)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Duplicadas != 1 {
		t.Errorf("duplicadas = %d, quer 1 (detalhe: %+v)", imp.Duplicadas, imp.Detalhe)
	}
	if len(st.cuponsSalvos) != 0 {
		t.Errorf("cupom confirmado foi sobrescrito: %+v", st.cuponsSalvos)
	}
}

func TestImportarAplicaValidacao(t *testing.T) {
	// Cupom cuja soma não fecha: tem de chegar com aviso e pendente.
	st := novoStoreFake()
	leitor := leitura.NovoLeitorGravado(
		respostaCupom("1340923", "37678845000736", "MATERNIDADE CÉLIA CÂMARA", 300.00,
			itemPaoFrances+","+itemPaoDoce)) // itens somam 285,00
	svc := NovoServico(st, leitor, logSilencioso())

	imp, err := svc.Importar(context.Background(),
		[]Arquivo{{Nome: "divergente.jpg", Dados: jpegCupom(t, 90)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if imp.ComPendencia != 1 {
		t.Errorf("com pendência = %d, quer 1", imp.ComPendencia)
	}

	c := st.cuponsSalvos[0]
	if !c.TemIncerto(dominio.CampoTotal) {
		t.Errorf("total deveria ficar incerto, incertos = %v", c.Incertos)
	}
	achou := false
	for _, a := range c.Avisos {
		if strings.Contains(a, "difere do total lido") {
			achou = true
		}
	}
	if !achou {
		t.Errorf("faltou o aviso de divergência: %v", c.Avisos)
	}
}

func TestImportarCadastraProdutoNovoComoAConfirmar(t *testing.T) {
	st := novoStoreFake()
	leitor := leitura.NovoLeitorGravado(
		respostaCupom("1340923", "37678845000736", "MATERNIDADE CÉLIA CÂMARA", 37.00,
			`{"cod":"3000","prod":"BOLO DE FUBÁ COM GOIABADA 90G","un":"UND","qtd":10,"vun":3.70,"vtot":37.00,"incertos":[]}`))
	svc := NovoServico(st, leitor, logSilencioso())

	if _, err := svc.Importar(context.Background(),
		[]Arquivo{{Nome: "novo.jpg", Dados: jpegCupom(t, 55)}}, nil); err != nil {
		t.Fatal(err)
	}

	if len(st.garantidos) != 1 || st.garantidos[0] != "3000" {
		t.Errorf("produtos cadastrados = %v, quer [3000]", st.garantidos)
	}
}

func TestImportarPDFCorrompidoNaoGastaLeitura(t *testing.T) {
	st := novoStoreFake()
	leitor := leitura.NovoLeitorGravado() // nenhuma resposta: se chamar, falha
	svc := NovoServico(st, leitor, logSilencioso())

	imp, err := svc.Importar(context.Background(),
		[]Arquivo{{Nome: "ruim.pdf", Dados: []byte("%PDF-1.4\nlixo sem página nenhuma")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Erros == 0 {
		t.Errorf("PDF inválido deveria gerar erro, veio %+v", imp)
	}
	if len(st.leituras) != 0 {
		t.Errorf("não deveria ter chamado a API: %d leituras", len(st.leituras))
	}
}

func TestImportarRespeitaLimiteDiario(t *testing.T) {
	st := novoStoreFake()
	st.leiturasHoje = 10
	leitor := leitura.NovoLeitorGravado()
	svc := NovoServico(st, leitor, logSilencioso())
	svc.LimiteDiario = 10

	imp, err := svc.Importar(context.Background(),
		[]Arquivo{{Nome: "a.jpg", Dados: jpegCupom(t, 60)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Status != "falhou" {
		t.Errorf("status = %q, quer falhou", imp.Status)
	}
	if len(st.leituras) != 0 {
		t.Errorf("o limite diário não barrou a chamada: %d leituras", len(st.leituras))
	}
	if len(imp.Detalhe) == 0 || !strings.Contains(imp.Detalhe[0].Erro, "limite diário") {
		t.Errorf("o motivo não ficou claro: %+v", imp.Detalhe)
	}
}

func TestImportarSemArquivos(t *testing.T) {
	svc := NovoServico(novoStoreFake(), leitura.NovoLeitorGravado(), logSilencioso())
	if _, err := svc.Importar(context.Background(), nil, nil); err == nil {
		t.Error("deveria recusar um lote vazio")
	}
}

// logCapturado coleta as linhas de log para os testes conferirem o que foi
// registrado.
type logCapturado struct{ linhas []string }

func (l *logCapturado) Write(p []byte) (int, error) {
	l.linhas = append(l.linhas, string(p))
	return len(p), nil
}

func (l *logCapturado) contem(sub string) bool {
	for _, x := range l.linhas {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

// leitorQueFalha devolve sempre erro, como aconteceu com a chave de gateway
// apontando para api.anthropic.com.
type leitorQueFalha struct{ msg string }

func (l leitorQueFalha) Ler(context.Context, imagem.Imagem, string, leitura.Referencias) (leitura.Resultado, error) {
	return leitura.Resultado{Modelo: "claude-opus-5"}, errors.New(l.msg)
}

func TestFalhaDeLeituraVaiParaOLog(t *testing.T) {
	// A falha era gravada só em leituras_log, no banco. Quem olhava o log do
	// contêiner não via nada, e a importação parecia não ter acontecido.
	cap := &logCapturado{}
	log := slog.New(slog.NewJSONHandler(cap, nil))

	st := novoStoreFake()
	svc := NovoServico(st, leitorQueFalha{msg: "a credencial foi recusada por https://api.anthropic.com"}, log)

	imp, err := svc.Importar(context.Background(),
		[]Arquivo{{Nome: "cupom.jpg", Dados: jpegCupom(t, 80)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Erros != 1 {
		t.Fatalf("erros = %d, quer 1", imp.Erros)
	}

	for _, esperado := range []string{
		"falha ao ler cupom",
		"cupom.jpg",
		"credencial foi recusada",
		"importação concluída",
	} {
		if !cap.contem(esperado) {
			t.Errorf("o log não registrou %q.\nlog: %v", esperado, cap.linhas)
		}
	}

	// E continua gravando no banco, para a auditoria de custo.
	if len(st.leituras) != 1 || st.leituras[0].Erro == "" {
		t.Errorf("a falha deveria continuar em leituras_log: %+v", st.leituras)
	}
}

func TestResumoDoLoteVaiParaOLog(t *testing.T) {
	cap := &logCapturado{}
	log := slog.New(slog.NewJSONHandler(cap, nil))

	st := novoStoreFake()
	leitor := leitura.NovoLeitorGravado(
		respostaCupom("1340923", "37678845000736", "MATERNIDADE CÉLIA CÂMARA", 120.00, itemPaoFrances))
	svc := NovoServico(st, leitor, log)

	if _, err := svc.Importar(context.Background(),
		[]Arquivo{{Nome: "ok.jpg", Dados: jpegCupom(t, 70)}}, nil); err != nil {
		t.Fatal(err)
	}

	for _, esperado := range []string{"importação iniciada", "importação concluída", "lidas"} {
		if !cap.contem(esperado) {
			t.Errorf("o log não registrou %q.\nlog: %v", esperado, cap.linhas)
		}
	}
}

func TestLimiteDiarioVaiParaOLog(t *testing.T) {
	cap := &logCapturado{}
	log := slog.New(slog.NewJSONHandler(cap, nil))

	st := novoStoreFake()
	st.leiturasHoje = 5
	svc := NovoServico(st, leitura.NovoLeitorGravado(), log)
	svc.LimiteDiario = 5

	if _, err := svc.Importar(context.Background(),
		[]Arquivo{{Nome: "a.jpg", Dados: jpegCupom(t, 60)}}, nil); err != nil {
		t.Fatal(err)
	}
	if !cap.contem("limite diário") {
		t.Errorf("o log não explicou o bloqueio.\nlog: %v", cap.linhas)
	}
}
