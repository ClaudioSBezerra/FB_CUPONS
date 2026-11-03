-- Esquema inicial do Cupons ITD.
--
-- Decisões que divergem do CLAUDE.md §4, de propósito (ver README):
--
--  * produtos usa id BIGSERIAL e codigo TEXT NULL, não "codigo TEXT PRIMARY
--    KEY". Nos dados reais existem dois produtos distintos sem código legível
--    (PÃO DOCE 25G da Maternidade e o 3º item do cupom 1342014 da Dona Íris),
--    e com o código como chave primária os dois não cabem na tabela.
--    Unicidade do código continua garantida por índice parcial.
--
--  * cupons usa índice único parcial em (numero, cnpj_cliente) em vez de
--    UNIQUE, porque numero pode ficar vazio quando está ilegível, e dois
--    cupons ilegíveis do mesmo cliente não são o mesmo cupom.

CREATE TABLE usuarios (
  id          BIGSERIAL PRIMARY KEY,
  email       TEXT NOT NULL UNIQUE,
  nome        TEXT NOT NULL DEFAULT '',
  senha_hash  TEXT NOT NULL,
  papel       TEXT NOT NULL DEFAULT 'operador' CHECK (papel IN ('admin','operador')),
  ativo       BOOLEAN NOT NULL DEFAULT TRUE,
  criado_em   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE clientes (
  id          BIGSERIAL PRIMARY KEY,
  nome        TEXT NOT NULL,
  cnpj        CHAR(14) NOT NULL UNIQUE,         -- só dígitos
  ativo       BOOLEAN NOT NULL DEFAULT TRUE,
  obs         TEXT NOT NULL DEFAULT '',
  criado_em   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE produtos (
  id          BIGSERIAL PRIMARY KEY,
  codigo      TEXT,                              -- NULL = sem código legível
  descricao   TEXT NOT NULL,
  unidade     TEXT NOT NULL CHECK (unidade IN ('UND','KG')),
  preco_ref   NUMERIC(14,2),
  confirmado  BOOLEAN NOT NULL DEFAULT TRUE,     -- FALSE = nome a confirmar
  criado_em   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX produtos_codigo_uniq ON produtos (codigo) WHERE codigo IS NOT NULL;
CREATE INDEX produtos_a_confirmar ON produtos (confirmado) WHERE confirmado = FALSE;

CREATE TABLE cupom_fotos (
  id          BIGSERIAL PRIMARY KEY,
  sha256      CHAR(64) NOT NULL UNIQUE,          -- evita reprocessar a mesma imagem
  mime        TEXT NOT NULL,
  largura     INT NOT NULL DEFAULT 0,
  altura      INT NOT NULL DEFAULT 0,
  bytes       INT NOT NULL DEFAULT 0,
  conteudo    BYTEA NOT NULL,
  criado_em   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE importacoes (
  id             BIGSERIAL PRIMARY KEY,
  arquivos       TEXT[] NOT NULL DEFAULT '{}',
  total_paginas  INT NOT NULL DEFAULT 0,
  lidas          INT NOT NULL DEFAULT 0,
  com_pendencia  INT NOT NULL DEFAULT 0,
  duplicadas     INT NOT NULL DEFAULT 0,
  erros          INT NOT NULL DEFAULT 0,
  status         TEXT NOT NULL DEFAULT 'processando'
                 CHECK (status IN ('processando','concluida','falhou')),
  detalhe        JSONB NOT NULL DEFAULT '[]',    -- resultado página a página
  usuario_id     BIGINT REFERENCES usuarios(id),
  criado_em      TIMESTAMPTZ NOT NULL DEFAULT now(),
  finalizado_em  TIMESTAMPTZ
);

CREATE TABLE cupons (
  id             BIGSERIAL PRIMARY KEY,
  numero         TEXT NOT NULL DEFAULT '',
  data           DATE,                           -- NULL quando ilegível
  cliente_id     BIGINT REFERENCES clientes(id),
  cnpj_cliente   CHAR(14),                       -- como lido/confirmado
  cliente_nome   TEXT NOT NULL DEFAULT '',
  emitente_cnpj  CHAR(14) NOT NULL DEFAULT '29220487000108',
  total          NUMERIC(14,2) NOT NULL DEFAULT 0,
  status         TEXT NOT NULL DEFAULT 'pendente'
                 CHECK (status IN ('pendente','confirmado','rejeitado')),
  incertos       TEXT[] NOT NULL DEFAULT '{}',   -- campos amarelos na revisão
  avisos         TEXT[] NOT NULL DEFAULT '{}',
  fonte          TEXT NOT NULL DEFAULT '',       -- ex.: "cupom-outubro.pdf p.7"
  obs            TEXT NOT NULL DEFAULT '',
  foto_id        BIGINT REFERENCES cupom_fotos(id),
  importacao_id  BIGINT REFERENCES importacoes(id),
  legado_id      TEXT,                           -- id do JSON do protótipo
  criado_em      TIMESTAMPTZ NOT NULL DEFAULT now(),
  atualizado_em  TIMESTAMPTZ NOT NULL DEFAULT now(),
  confirmado_em  TIMESTAMPTZ,
  confirmado_por BIGINT REFERENCES usuarios(id)
);

-- Impede duplicidade na reimportação, sem confundir dois cupons cujo número
-- está ilegível.
CREATE UNIQUE INDEX cupons_numero_cliente_uniq
  ON cupons (numero, cnpj_cliente)
  WHERE numero <> '' AND cnpj_cliente IS NOT NULL;

CREATE UNIQUE INDEX cupons_legado_uniq ON cupons (legado_id) WHERE legado_id IS NOT NULL;

CREATE INDEX cupons_status ON cupons (status);
CREATE INDEX cupons_competencia ON cupons (date_trunc('month', data), cliente_id);
CREATE INDEX cupons_cliente ON cupons (cliente_id);

CREATE TABLE cupom_itens (
  id          BIGSERIAL PRIMARY KEY,
  cupom_id    BIGINT NOT NULL REFERENCES cupons(id) ON DELETE CASCADE,
  ordem       INT NOT NULL,
  codigo      TEXT NOT NULL DEFAULT '',
  produto     TEXT NOT NULL,
  unidade     TEXT NOT NULL DEFAULT 'UND' CHECK (unidade IN ('UND','KG')),
  qtd         NUMERIC(14,3) NOT NULL DEFAULT 0,
  vun         NUMERIC(14,2) NOT NULL DEFAULT 0,
  vtot        NUMERIC(14,2) NOT NULL DEFAULT 0,
  incertos    TEXT[] NOT NULL DEFAULT '{}',
  UNIQUE (cupom_id, ordem)
);

CREATE INDEX cupom_itens_cupom ON cupom_itens (cupom_id);
CREATE INDEX cupom_itens_codigo ON cupom_itens (codigo);

-- Auditoria e custo de cada chamada ao Claude.
CREATE TABLE leituras_log (
  id             BIGSERIAL PRIMARY KEY,
  foto_id        BIGINT REFERENCES cupom_fotos(id),
  cupom_id       BIGINT REFERENCES cupons(id) ON DELETE SET NULL,
  importacao_id  BIGINT REFERENCES importacoes(id),
  modelo         TEXT NOT NULL DEFAULT '',
  tokens_in      INT NOT NULL DEFAULT 0,
  tokens_out     INT NOT NULL DEFAULT 0,
  duracao_ms     INT NOT NULL DEFAULT 0,
  resposta_bruta JSONB,
  erro           TEXT,
  criado_em      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX leituras_log_dia ON leituras_log (date_trunc('day', criado_em));
