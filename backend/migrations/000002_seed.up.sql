-- Seed de clientes e catálogo de produtos (CLAUDE.md §2).
--
-- Idempotente: rodar de novo não duplica nem sobrescreve edições feitas pela
-- interface.

INSERT INTO clientes (nome, cnpj, obs) VALUES
  ('MATERNIDADE CÉLIA CÂMARA', '37678845000736', 'Instituto Patris 3808'),
  ('DONA ÍRIS',                '37678845000302', 'Instituto Patris 3697, Goiânia'),
  ('ARROZ CRISTAL',            '02709992000156', 'cliente cadastrado, sem cupons ainda')
ON CONFLICT (cnpj) DO NOTHING;

INSERT INTO produtos (codigo, descricao, unidade, preco_ref, confirmado) VALUES
  ('20771', 'PÃO FRANCÊS 50G',                          'UND',  1.00, TRUE),
  ('22121', 'PÃO DOCE 50G',                             'UND',  1.50, TRUE),
  ('2755',  'ROSCAS 80G',                               'UND',  3.30, TRUE),
  ('1594',  'ROSCAS 80G DIET/INTEGRAL',                 'UND',  4.00, TRUE),
  ('1645',  'ROSCAS 50G',                               'UND',  3.00, TRUE),
  ('3629',  'BOLOS 90G',                                'UND',  4.15, TRUE),
  ('941',   'BROA 80G',                                 'UND',  3.30, TRUE),
  ('210',   'SALGADINHOS VARIADOS 30G',                 'UND',  1.10, TRUE),
  ('836',   'PÃO DE QUEIJO CONGELADO',                  'KG',  29.99, TRUE),
  ('673',   'BISCOITO DE QUEIJO CONGELADO KG',          'KG',  29.99, TRUE),
  ('213',   'BOLO DE CENOURA',                          'KG',  35.99, TRUE),
  ('3636',  'PÃO DE FORMA 500G',                        'UND',  9.99, TRUE),
  ('2431',  'PÃO DE QUEIJO 80G',                        'UND',  3.30, TRUE),
  ('383',   'BISCOITO DE QUEIJO 80G',                   'UND',  3.30, TRUE),
  ('1207',  'AMERICANO DE FRANGO 100G',                 'UND',  5.00, TRUE),
  ('232',   'AMERICANO DE FRANGO DIET/INTEGRAL 100G',   'UND',  5.50, TRUE),
  ('2045',  'ENROLADINHO DE QUEIJO 100G',               'UND',  5.00, TRUE),
  ('1485',  'ENROLADINHO DE QUEIJO DIET/INTEGRAL 100G', 'UND',  5.50, TRUE),
  ('2077',  'PETIT GATEAU',                             'UND',  9.90, TRUE),
  ('1534',  'PÃO FRANCÊS 25G',                          'UND',  0.50, TRUE),
  ('288',   'PÃO DE QUEIJO',                            'KG',  45.99, TRUE),
  ('110',   'ENROLADINHO DE QUEIJO KG',                 'KG',  45.99, TRUE)
ON CONFLICT (codigo) WHERE codigo IS NOT NULL DO NOTHING;

-- Produtos que aparecem nos dados reais mas não estão no catálogo oficial.
-- Entram com confirmado = FALSE para aparecerem na tela de Cadastros como
-- pendentes de confirmação com o cliente.
--
-- 1930 e 2156: códigos da Dona Íris cujo nome está ilegível na foto.
-- 3000: aparece na Maternidade com descrição legível, mas não consta no
-- catálogo do CLAUDE.md — preço de referência deduzido (50 × 3,70 = 185,00).
INSERT INTO produtos (codigo, descricao, unidade, preco_ref, confirmado) VALUES
  ('1930', 'PRODUTO 1930 (nome ilegível na foto)', 'UND', 3.30, FALSE),
  ('2156', 'PRODUTO 2156 (nome ilegível na foto)', 'UND', NULL, FALSE),
  ('3000', 'BOLO DE FUBÁ COM GOIABADA 90G',        'UND', 3.70, FALSE)
ON CONFLICT (codigo) WHERE codigo IS NOT NULL DO NOTHING;
