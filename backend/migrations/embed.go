// Package migrations carrega o SQL versionado para dentro do binário, de modo
// que subir o container já deixe o esquema correto — sem passo manual no
// deploy e sem risco de a imagem rodar com migration de outra versão.
package migrations

import "embed"

// Arquivos são as migrations, aplicadas em ordem pelo golang-migrate.
//
//go:embed *.sql
var Arquivos embed.FS
