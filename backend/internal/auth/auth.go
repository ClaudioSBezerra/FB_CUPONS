// Package auth trata login e sessão. A sessão é um JWT em cookie httpOnly:
// nada de token em localStorage, que vaza por XSS.
package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// NomeCookie é o nome do cookie de sessão.
const NomeCookie = "cupons_sessao"

// ErroCredenciais é devolvido quando e-mail ou senha não conferem. A mensagem
// é deliberadamente genérica: dizer "usuário não existe" entrega quais
// e-mails estão cadastrados.
var ErroCredenciais = errors.New("e-mail ou senha incorretos")

// Gerenciador emite e valida sessões.
type Gerenciador struct {
	segredo  []byte
	duracao  time.Duration
	producao bool
}

// NovoGerenciador monta o gerenciador de sessões.
func NovoGerenciador(segredo string, duracao time.Duration, producao bool) *Gerenciador {
	if duracao <= 0 {
		duracao = 12 * time.Hour
	}
	return &Gerenciador{segredo: []byte(segredo), duracao: duracao, producao: producao}
}

// Sessao é o conteúdo do token.
type Sessao struct {
	UsuarioID int64
	Email     string
	Papel     string
}

// HashSenha gera o hash bcrypt de uma senha.
func HashSenha(senha string) (string, error) {
	if len(senha) < 8 {
		return "", fmt.Errorf("a senha precisa ter pelo menos 8 caracteres")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(senha), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// ConferirSenha compara a senha informada com o hash guardado.
func ConferirSenha(hash, senha string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(senha)); err != nil {
		return ErroCredenciais
	}
	return nil
}

// Emitir cria o token da sessão.
func (g *Gerenciador) Emitir(s Sessao) (string, time.Time, error) {
	expira := time.Now().Add(g.duracao)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   strconv.FormatInt(s.UsuarioID, 10),
		"email": s.Email,
		"papel": s.Papel,
		"exp":   expira.Unix(),
		"iat":   time.Now().Unix(),
	})
	assinado, err := token.SignedString(g.segredo)
	if err != nil {
		return "", time.Time{}, err
	}
	return assinado, expira, nil
}

// Validar confere a assinatura e a validade do token.
func (g *Gerenciador) Validar(assinado string) (Sessao, error) {
	token, err := jwt.Parse(assinado, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("método de assinatura inesperado: %v", t.Header["alg"])
		}
		return g.segredo, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return Sessao{}, fmt.Errorf("sessão inválida ou expirada: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return Sessao{}, errors.New("sessão inválida")
	}

	sub, _ := claims["sub"].(string)
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil {
		return Sessao{}, errors.New("sessão sem identificação de usuário")
	}
	email, _ := claims["email"].(string)
	papel, _ := claims["papel"].(string)

	return Sessao{UsuarioID: id, Email: email, Papel: papel}, nil
}

// GravarCookie põe o cookie de sessão na resposta.
func (g *Gerenciador) GravarCookie(w http.ResponseWriter, token string, expira time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     NomeCookie,
		Value:    token,
		Path:     "/",
		Expires:  expira,
		HttpOnly: true,
		Secure:   g.producao,
		SameSite: http.SameSiteLaxMode,
	})
}

// LimparCookie encerra a sessão no navegador.
func (g *Gerenciador) LimparCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     NomeCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   g.producao,
		SameSite: http.SameSiteLaxMode,
	})
}
