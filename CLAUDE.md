# Cupons ITD — Sistema de cupons fiscais e cobrança mensal

Sistema web para processar **cupons fiscais térmicos** emitidos pela **ITD Padaria e Confeitaria Eireli – ME (STRAVAGANZA, CNPJ 29.220.487/0001-08)** aos seus clientes, e gerar a **cobrança mensal** (Excel) por cliente e produto.

Stack: **Go (API) + PostgreSQL + React (frontend)**, deploy em **VPS Hostinger**, no mesmo padrão do projeto SmartPick (seguir a estrutura de pastas, Dockerfile/compose, CI e convenções dele quando o repositório estiver disponível).
Prioridade: **sistema crítico — não pode haver erro de CNPJ, valor ou quantidade.** Toda dúvida vai para revisão humana; nunca "chutar" silenciosamente.

Idioma: interface, mensagens, logs voltados ao usuário e comentários em **português do Brasil**. Valores em R$ com vírgula na tela; `NUMERIC(14,2)` no banco.

---

## 1. Origem: protótipo existente

Existe um protótipo single-file HTML (`legado/app-cupons-v5.html`) que roda no navegador com OCR Tesseract e IndexedDB. **O OCR falha em cupons apagados** (impressão térmica matricial fraca), especialmente da cliente Dona Íris. Por isso a leitura passa a ser feita pela **API do Claude (visão)**, no backend. A leitura feita manualmente pelo Claude (visão) foi muito superior ao OCR e é a referência de qualidade.

O protótipo contém as regras de negócio a portar (seção 5 e 6) e dois JSONs de dados reais a importar (seção 9):
- `legado/cupons-outubro-2026-itens.json` — 18 cupons da Maternidade Célia Câmara, total R$ 7.225,77
- `legado/dona-iris-outubro-2026.json` — 17 cupons da Dona Íris (12 confirmados = R$ 11.500,46; 5 pendentes)

**Não quebrar o que já está correto:** a base desses JSONs está correta e é a referência de testes.

---

## 2. Domínio

- **Emitente (fornecedor):** STRAVAGANZA / ITD Padaria e Confeitaria Eireli – ME, CNPJ `29.220.487/0001-08`.
- **Cupom fiscal (térmico, 1 por página nos PDFs escaneados no iPhone):** número, data, CNPJ do cliente, nome do cliente, lista de itens e total.
- **Item:** código, descrição do produto, unidade (UND/KG), quantidade, valor unitário, valor total do item.
- **Cobrança mensal:** soma, por **mês + cliente + produto (+ unidade)**, de quantidade, valor e nº de cupons. Cada cliente recebe sua cobrança de um mês.

### Clientes conhecidos (seed)

| Cliente | CNPJ | Observação |
|---|---|---|
| MATERNIDADE CÉLIA CÂMARA | 37.678.845/0007-36 | Instituto Patris 3808 |
| DONA ÍRIS | 37.678.845/0003-02 | Instituto Patris 3697, Goiânia |
| ARROZ CRISTAL | 02.709.992/0001-56 | cliente cadastrado, sem cupons ainda |

Os dois primeiros CNPJs diferem em apenas 3 dígitos. **Nunca** "corrigir" um CNPJ lido para o conhecido mais próximo se houver empate; só aceitar correção quando houver **um único** CNPJ conhecido a distância mínima (Hamming ≤ 3) **e** o resultado tiver dígito verificador válido. Caso contrário, manter o lido e marcar como incerto.

### Catálogo de produtos (seed; código → descrição, unidade, preço unitário padrão)

| Código | Produto | Un | Preço |
|---|---|---|---|
| 20771 | PÃO FRANCÊS 50G | UND | 1,00 |
| 22121 | PÃO DOCE 50G | UND | 1,50 |
| 2755 | ROSCAS 80G | UND | 3,30 |
| 1594 | ROSCAS 80G DIET/INTEGRAL | UND | 4,00 |
| 1645 | ROSCAS 50G | UND | 3,00 |
| 3629 | BOLOS 90G | UND | 4,15 |
| 941 | BROA 80G | UND | 3,30 |
| 210 | SALGADINHOS VARIADOS 30G | UND | 1,10 |
| 836 | PÃO DE QUEIJO CONGELADO | KG | 29,99 |
| 673 | BISCOITO DE QUEIJO CONGELADO KG | KG | 29,99 |
| 213 | BOLO DE CENOURA | KG | 35,99 |
| 3636 | PÃO DE FORMA 500G | UND | 9,99 |
| 2431 | PÃO DE QUEIJO 80G | UND | 3,30 |
| 383 | BISCOITO DE QUEIJO 80G | UND | 3,30 |
| 1207 | AMERICANO DE FRANGO 100G | UND | 5,00 |
| 232 | AMERICANO DE FRANGO DIET/INTEGRAL 100G | UND | 5,50 |
| 2045 | ENROLADINHO DE QUEIJO 100G | UND | 5,00 |
| 1485 | ENROLADINHO DE QUEIJO DIET/INTEGRAL 100G | UND | 5,50 |
| 2077 | PETIT GATEAU | UND | 9,90 |
| 1534 | PÃO FRANCÊS 25G | UND | 0,50 |
| 288 | PÃO DE QUEIJO | KG | 45,99 |
| 110 | ENROLADINHO DE QUEIJO KG | KG | 45,99 |

Códigos com nome ilegível na foto (a confirmar com o cliente): **1930, 2156, 1645 (Dona Íris)**. Nos dados existem como `PRODUTO <cod> (nome ilegível na foto)`.

O catálogo é **editável pela interface** (CRUD) e o preço unitário do catálogo é só **referência de validação**; o preço oficial é o que está impresso no cupom.

---

## 3. Arquitetura

```
cupons-itd/
  CLAUDE.md
  legado/                 protótipo HTML + JSONs de referência (somente leitura)
  backend/                Go
    cmd/api/main.go
    internal/
      config/             env vars
      http/               handlers, middlewares (auth, cors, logging), rotas
      auth/               login + JWT (ou sessão por cookie httpOnly)
      leitura/            cliente Claude API, prompt, parsing do JSON
      validacao/          regras (CNPJ, aritmética, catálogo, datas)
      pdf/                split de PDF em páginas/imagens
      cobranca/           agregações
      exportxlsx/         geração do Excel (excelize)
      store/              acesso ao Postgres (pgx + sqlc ou similar)
    migrations/           SQL versionado (golang-migrate)
  frontend/               React + TypeScript + Vite
  deploy/                 docker-compose.yml, nginx/caddy, scripts
```

Preferências técnicas:
- **Go ≥ 1.22**, `net/http` + `chi`, `pgx`, `golang-migrate`, `excelize`, logs estruturados (`slog`). Testes com `go test` (table-driven). Sem frameworks pesados.
- **PDF:** extrair páginas como imagem. Preferir `pdftoppm` (poppler-utils) ou `pdfcpu`/`mupdf` via binário no container. Alternativa já validada no protótipo: PDFs gerados por escâner de iPhone contêm um JPEG (DCTDecode) por página — extrair direto sem recompressão quando possível.
- **React + TypeScript + Vite**, TanStack Query, UI simples e responsiva (uso em notebook; upload também pelo celular).
- **PostgreSQL 16.** Secrets só em variáveis de ambiente (`ANTHROPIC_API_KEY`, `DATABASE_URL`, `JWT_SECRET`). **Nunca** commitar chaves.
- **Deploy:** Docker Compose no VPS Hostinger (api + postgres + proxy reverso com HTTPS/Let's Encrypt). Backup diário do Postgres (`pg_dump`) com retenção.

---

## 4. Modelo de dados (migração inicial)

```sql
CREATE TABLE clientes (
  id          BIGSERIAL PRIMARY KEY,
  nome        TEXT NOT NULL,
  cnpj        CHAR(14) NOT NULL UNIQUE,        -- só dígitos
  ativo       BOOLEAN NOT NULL DEFAULT TRUE,
  criado_em   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE produtos (
  codigo      TEXT PRIMARY KEY,
  descricao   TEXT NOT NULL,
  unidade     TEXT NOT NULL CHECK (unidade IN ('UND','KG')),
  preco_ref   NUMERIC(14,2),
  confirmado  BOOLEAN NOT NULL DEFAULT TRUE    -- FALSE = nome ilegível a confirmar
);

CREATE TABLE cupons (
  id            BIGSERIAL PRIMARY KEY,
  numero        TEXT NOT NULL,
  data          DATE NOT NULL,
  cliente_id    BIGINT REFERENCES clientes(id),
  cnpj_cliente  CHAR(14),                       -- como lido/confirmado
  emitente_cnpj CHAR(14) NOT NULL DEFAULT '29220487000108',
  total         NUMERIC(14,2) NOT NULL DEFAULT 0,
  status        TEXT NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente','confirmado','rejeitado')),
  incertos      TEXT[] NOT NULL DEFAULT '{}',   -- campos amarelos: data, num, total, cnpjCli, cli...
  avisos        TEXT[] NOT NULL DEFAULT '{}',
  fonte         TEXT,                           -- ex.: "PDF cupom-outubro.pdf p.7"
  obs           TEXT,
  foto_id       BIGINT,                         -- FK para cupom_fotos
  criado_em     TIMESTAMPTZ NOT NULL DEFAULT now(),
  confirmado_em TIMESTAMPTZ,
  confirmado_por BIGINT,
  UNIQUE (numero, cnpj_cliente)                 -- impede duplicidade na reimportação
);

CREATE TABLE cupom_itens (
  id          BIGSERIAL PRIMARY KEY,
  cupom_id    BIGINT NOT NULL REFERENCES cupons(id) ON DELETE CASCADE,
  ordem       INT NOT NULL,
  codigo      TEXT,
  produto     TEXT NOT NULL,
  unidade     TEXT,
  qtd         NUMERIC(14,3) NOT NULL,
  vun         NUMERIC(14,2) NOT NULL,
  vtot        NUMERIC(14,2) NOT NULL,
  incertos    TEXT[] NOT NULL DEFAULT '{}'      -- cod, prod, qtd, vun, vtot
);

CREATE TABLE cupom_fotos (
  id        BIGSERIAL PRIMARY KEY,
  sha256    CHAR(64) NOT NULL UNIQUE,           -- evita reprocessar a mesma imagem
  mime      TEXT NOT NULL,
  conteudo  BYTEA NOT NULL                      -- ou caminho em disco/S3 se preferir
);

CREATE TABLE usuarios (
  id BIGSERIAL PRIMARY KEY, email TEXT UNIQUE NOT NULL,
  senha_hash TEXT NOT NULL, papel TEXT NOT NULL DEFAULT 'operador'
);

CREATE TABLE leituras_log (                      -- auditoria/custo de cada chamada ao Claude
  id BIGSERIAL PRIMARY KEY, foto_id BIGINT, modelo TEXT, tokens_in INT, tokens_out INT,
  resposta_bruta JSONB, erro TEXT, criado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Regras de modelagem: CNPJ sempre armazenado só com dígitos, exibido formatado `00.000.000/0000-00`. Data nunca com fuso (tipo `DATE`).

---

## 5. Leitura via Claude (visão)

Fluxo `POST /api/importacoes` (multipart; PDF ou imagens):
1. Receber arquivos; calcular SHA-256 de cada imagem; se já existir em `cupom_fotos`, avisar "já importada" e **não** duplicar.
2. PDF → uma imagem por página. Imagem solta = 1 cupom (fotos altas de celular **não** devem ser fatiadas em vários cupons; só segmentar se a página claramente tiver vários cupons lado a lado).
3. Para cada imagem, chamar a API Messages do Claude com a imagem e instrução para devolver **somente JSON** (use tool/JSON schema para saída estruturada). Modelo configurável por env (`CLAUDE_MODEL`), padrão um modelo de visão atual de bom custo-benefício; opção de reprocessar um cupom difícil com modelo mais forte. Timeout e retry com backoff; limitar concorrência (ex.: 3).
4. Aplicar as validações (seção 6). Criar o cupom com `status='pendente'` e preencher `incertos`/`avisos`.
5. Responder com o resumo do lote (quantos lidos, quantos com pendências, erros).

### Prompt de leitura (base)

> Você lê cupons fiscais térmicos de uma padaria (emitente STRAVAGANZA / ITD Padaria e Confeitaria, CNPJ 29.220.487/0001-08). A imagem contém UM cupom. Impressão matricial/térmica, às vezes apagada. Extraia: número do cupom, data (dd/mm/aaaa), CNPJ e nome do cliente, TODOS os itens (código, descrição, unidade, quantidade, valor unitário, valor total do item) e o total do cupom. Regras: (1) nunca invente — se um campo estiver ilegível, devolva `null` e liste o campo em `incertos`; (2) leia os números dígito a dígito; (3) quantidade × unitário deve igualar o valor do item; a soma dos itens deve igualar o total — se não bater, reavalie a leitura antes de responder e registre o que continuar divergente em `avisos`; (4) códigos de produto conhecidos: <catálogo>; use-os para desambiguar descrições, mas não substitua um código ilegível por um chute; (5) CNPJs de clientes conhecidos: <clientes>; só use um deles se os dígitos lidos forem compatíveis. Responda apenas com o JSON do schema.

### Schema de saída esperado

```json
{
  "numero": "1340923",
  "data": "2026-10-16",
  "cnpj_cliente": "37678845000736",
  "cliente": "MATERNIDADE CÉLIA CÂMARA",
  "total": 435.00,
  "itens": [
    {"cod":"20771","prod":"PÃO FRANCÊS 50G","un":"UND","qtd":120,"vun":1.00,"vtot":120.00,"incertos":[]}
  ],
  "incertos": ["data","numero"],
  "avisos": []
}
```

**Número e data sempre entram em `incertos`** (são os campos que mais erram em cupom apagado) para forçar o olho humano na Revisão.

---

## 6. Regras de validação (portar do protótipo; cobrir com testes)

1. **CNPJ:** apenas dígitos; 14 posições; **dígito verificador válido** (módulo 11, pesos oficiais). CNPJ do cliente inválido → campo incerto + aviso. Se inválido mas há **um único** CNPJ conhecido a Hamming ≤ 3 cujo DV é válido → sugerir (não aplicar automaticamente em lote sem marcar como incerto). CNPJ do emitente deve ser `29.220.487/0001-08`; divergência leve (Hamming ≤ 3) é ajustada com aviso.
2. **Aritmética por item:** `qtd × vun == vtot` (tolerância R$ 0,01). Se divergir, marcar o item e tentar reconciliar: usar preço do catálogo para deduzir o campo ilegível (ex.: vtot/vun = qtd); só aplicar se o resultado for inteiro/consistente e marcar `incertos`.
3. **Soma do cupom:** `Σ vtot == total` (tolerância R$ 0,01). Divergência → aviso "A soma dos itens (X) difere do total lido (Y)". Se **exatamente um** valor ilegível/incerto explicar a diferença, sugerir o valor deduzido (ex.: item 2156 = R$ 64,50 na Dona Íris) e manter pendente.
4. **Código → catálogo:** código exato → preenche descrição/unidade. Código fora do catálogo → aceita, cria produto com `confirmado=false`. Edição de distância 1 para um código do catálogo → sugerir, nunca aplicar silenciosamente. Se a descrição lida for ≥ 55% similar à do catálogo, usar a do catálogo.
5. **Datas:** formato `dd/mm/aaaa`; se o cupom tiver datas diferentes (impressão da venda vs. emissão) → aviso "datas diferentes no cupom". Ano fora de [mês de referência ± 1] → suspeito (OCR leu 2025 em cupons de 2026): marcar incerto.
6. **Duplicidade:** `UNIQUE(numero, cnpj_cliente)`. Reimportar o mesmo cupom **atualiza** apenas se o existente estiver `pendente`; se `confirmado`, não sobrescrever (retornar conflito para a UI).
7. Total `0` ou sem itens → sempre `pendente`.
8. Um cupom só pode ir a `confirmado` se: CNPJ válido, itens ≥ 1, aritmética por item ok, soma = total (ou o operador confirmar explicitamente a divergência com justificativa gravada em `obs`).

---

## 7. API (REST/JSON, prefixo `/api`)

- `POST /auth/login`, `POST /auth/logout`, `GET /me`
- `POST /importacoes` (multipart) · `GET /importacoes/{id}`
- `GET /cupons?mes=2026-10&cliente_id=&status=` · `GET /cupons/{id}` · `PUT /cupons/{id}` (edição) · `POST /cupons/{id}/confirmar` · `POST /cupons/{id}/rejeitar` · `POST /cupons/{id}/reprocessar` (nova leitura, opcional com modelo mais forte) · `GET /cupons/{id}/foto`
- CRUD `/clientes`, `/produtos`
- `GET /cobranca?mes=2026-10&cliente_id=&agrupar=produto|cupom` (JSON)
- `GET /cobranca/export.xlsx?mes=&cliente_id=` (Excel)
- `POST /legado/importar` (JSON do protótipo, formato `{versao, cupons:[...]}`, idempotente por `id`/`(numero,cnpj)`).

Autenticação obrigatória em tudo; papéis `admin` e `operador`. Rate limit nas importações. Tamanho máximo de upload configurável (ex.: 50 MB).

---

## 8. Frontend (React)

Abas/páginas, espelhando o protótipo:
1. **Importar** — arrastar PDF/fotos; barra de progresso por página; resultado do lote.
2. **Revisão** — lista de cupons `pendente`; ao abrir: foto grande (zoom/rotação) ao lado do formulário; campos incertos em **amarelo**; editor de itens (adicionar/remover linha, autocompletar pelo código do catálogo); totais e avisos recalculados ao vivo; botões Confirmar / Rejeitar / Reprocessar. Navegação "próximo pendente" por teclado.
3. **Cupons** — filtro por mês, cliente, status; busca por número.
4. **Cobrança** — escolher mês e cliente; tabela por produto (qtd, nº cupons, valor) com total geral; botão "Exportar Excel". Totais por cliente conferem com a soma dos cupons confirmados.
5. **Cadastros** — clientes, produtos (marcar produtos `confirmado=false` pendentes de nome).
6. **Admin** — usuários, log de leituras (tokens/custo).

Valores sempre formatados `pt-BR`. Não perder edição em andamento ao trocar de aba.

---

## 9. Exportação Excel (excelize)

Arquivo `cobranca-AAAA-MM[-cliente].xlsx` com abas:
- **Cobrança:** Mês | Cliente | CNPJ | Código | Produto | Un | Qtd total | Nº cupons | Valor total; linha de total por cliente.
- **Itens:** um registro por item de cupom (cupom, data, cliente, cód, produto, un, qtd, vun, vtot).
- **Cupons:** nº, data, cliente, CNPJ, total, status.

Somente cupons `confirmado` entram na cobrança (pendentes ficam fora, e a tela avisa quantos faltam). Datas gravadas como data pura (sem fuso — no protótipo um bug fez datas aparecerem um dia antes). Valores como número com formato monetário, não texto.

---

## 10. Migração dos dados do legado (critério de aceite)

Após `POST /legado/importar` dos dois JSONs, a base deve fechar **exatamente**:
- **Maternidade Célia Câmara, outubro/2026:** 18 cupons, **R$ 7.225,77** (cupom 1180222 = R$ 539,90 — o valor 339,90 era erro de leitura antigo).
- **Dona Íris, outubro/2026:** 12 confirmados = **R$ 11.500,46**, 5 pendentes (nº 1341236 total 926,98 com item 2156 deduzido = 64,50; nº 1341386 total 1.005,00; nº 1341916 total 549,98; nº 1342014 total 577,50 com 3º item ilegível; nº 1180221 total 0, item 3 e total ilegíveis).
- **Total confirmado dos dois clientes: R$ 18.726,23.**
- Reimportar os mesmos arquivos não pode duplicar nada (atenção: cupom nº 1342024 já gerou suspeita de duplicidade).

---

## 11. Testes obrigatórios

- Unitários (table-driven): validação de CNPJ (válidos, inválidos, empate de Hamming entre os dois CNPJs conhecidos); aritmética de item e de cupom; reconciliação por catálogo; datas; agrupamento da cobrança.
- Integração (Postgres de teste via docker): importar os JSONs do legado e conferir os totais da seção 10; unicidade na reimportação; confirmação bloqueada quando não valida.
- Leitura: testes com **cliente Claude mockado** (respostas gravadas) para não gastar API em CI; um teste manual/opcional `-tags=live` com `ANTHROPIC_API_KEY` real sobre algumas páginas do PDF da Dona Íris, comparando com o JSON de referência (taxa de acerto de nº, total e itens).
- Excel: abrir o `.xlsx` gerado e conferir somas e datas.
- Frontend: testes dos fluxos Revisão e Cobrança (Vitest + Testing Library); e2e básico com Playwright.

---

## 12. Segurança, LGPD e operação

- HTTPS obrigatório; cookies `httpOnly`/`Secure`/`SameSite`; senhas com argon2/bcrypt; CORS restrito.
- Chave da Anthropic **somente no servidor**; nunca enviar ao frontend nem logar. Logs sem dados sensíveis desnecessários.
- Fotos de cupons contêm CNPJ de clientes: acesso só autenticado, backup criptografado, política de retenção definida.
- Healthcheck `/healthz`, métricas básicas, alerta de falha de backup.
- Controle de custo: registrar tokens por leitura; limite diário/mensal configurável de chamadas.

---

## 13. Plano de execução (marcos)

1. **Esqueleto:** repo, Docker Compose (api + postgres), migrations, `/healthz`, auth.
2. **Domínio:** pacote `validacao` com todos os testes (CNPJ, aritmética, catálogo, datas) — sem depender de IA.
3. **Legado:** endpoint de importação dos JSONs + critério de aceite da seção 10 passando.
4. **Cobrança + Excel:** agregações SQL e `exportxlsx`; comparar com `cobranca-2026-10-clientes.xlsx` de referência.
5. **Leitura Claude:** cliente da API, prompt, schema, PDF → imagens, log de custo; testes com mock e `live`.
6. **Frontend:** Importar → Revisão → Cobrança → Cadastros.
7. **Deploy no VPS:** compose de produção, proxy HTTPS, backup, domínio; roteiro de rollback.
8. **Piloto:** processar o PDF real da Dona Íris com a leitura via Claude, medir acerto vs. JSON de referência e ajustar prompt/validações.

## 14. Como trabalhar neste repositório

- Comece sempre pelo marco da vez, rode `go vet`, `go test ./...` e o lint do frontend antes de dar algo como pronto.
- Não alterar nada em `legado/`. Qualquer divergência encontrada nos dados de referência deve ser **reportada**, não "corrigida" em silêncio.
- Nunca relaxar uma validação para fazer um teste passar; se uma regra estiver errada, explique e proponha a mudança.
- Perguntar ao usuário antes de: mudar o modelo de dados já em produção, apagar dados, alterar a lógica de cobrança ou gastar API em lote grande.
- Commits pequenos e em português, mensagens objetivas.
