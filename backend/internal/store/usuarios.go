package store

import (
	"context"
	"fmt"
	"strings"
)

// Usuario é quem acessa o sistema.
type Usuario struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Nome      string `json:"nome"`
	Papel     string `json:"papel"`
	Ativo     bool   `json:"ativo"`
	SenhaHash string `json:"-"` // nunca sai em JSON
}

// UsuarioPorEmail busca o usuário para o login.
func (s *Store) UsuarioPorEmail(ctx context.Context, email string) (Usuario, error) {
	var u Usuario
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, nome, papel, ativo, senha_hash FROM usuarios WHERE lower(email) = lower($1)`,
		strings.TrimSpace(email),
	).Scan(&u.ID, &u.Email, &u.Nome, &u.Papel, &u.Ativo, &u.SenhaHash)
	if err != nil {
		return Usuario{}, semLinhas(err)
	}
	return u, nil
}

// UsuarioPorID busca o usuário autenticado pelo id do token.
func (s *Store) UsuarioPorID(ctx context.Context, id int64) (Usuario, error) {
	var u Usuario
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, nome, papel, ativo, senha_hash FROM usuarios WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Nome, &u.Papel, &u.Ativo, &u.SenhaHash)
	if err != nil {
		return Usuario{}, semLinhas(err)
	}
	return u, nil
}

// ListarUsuarios devolve todos os usuários (tela de Admin).
func (s *Store) ListarUsuarios(ctx context.Context) ([]Usuario, error) {
	linhas, err := s.pool.Query(ctx,
		`SELECT id, email, nome, papel, ativo FROM usuarios ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer linhas.Close()

	out := []Usuario{}
	for linhas.Next() {
		var u Usuario
		if err := linhas.Scan(&u.ID, &u.Email, &u.Nome, &u.Papel, &u.Ativo); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, linhas.Err()
}

// CriarUsuario insere um usuário com a senha já hasheada.
func (s *Store) CriarUsuario(ctx context.Context, u Usuario) (Usuario, error) {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO usuarios (email, nome, senha_hash, papel, ativo)
		 VALUES (lower($1), $2, $3, $4, $5) RETURNING id`,
		strings.TrimSpace(u.Email), u.Nome, u.SenhaHash, u.Papel, u.Ativo,
	).Scan(&u.ID)
	if ehViolacaoUnicidade(err) {
		return Usuario{}, &ErroConflito{Msg: fmt.Sprintf("já existe usuário com o e-mail %s", u.Email)}
	}
	if err != nil {
		return Usuario{}, err
	}
	return u, nil
}

// GarantirAdmin cria o usuário administrador inicial se a tabela estiver
// vazia. Chamado na subida, com as credenciais do ambiente.
func (s *Store) GarantirAdmin(ctx context.Context, email, nome, senhaHash string) (criado bool, err error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM usuarios`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	_, err = s.CriarUsuario(ctx, Usuario{
		Email: email, Nome: nome, SenhaHash: senhaHash, Papel: "admin", Ativo: true,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
