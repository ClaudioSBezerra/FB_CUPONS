# Cupons ITD

Sistema web para ler **cupons fiscais térmicos** emitidos pela ITD Padaria e Confeitaria Eireli – ME
(STRAVAGANZA, CNPJ 29.220.487/0001-08) e gerar a **cobrança mensal em Excel** por cliente e produto.

A leitura dos cupons é feita pela **API do Claude (visão)** no backend — o OCR do protótipo falhava em
impressão térmica apagada. Toda dúvida vai para revisão humana; nada é "chutado" em silêncio.

> **Sistema crítico:** não pode haver erro de CNPJ, valor ou quantidade.

## Stack

| Camada   | Tecnologia                                                        |
|----------|-------------------------------------------------------------------|
| Backend  | Go ≥ 1.22 · `net/http` + chi · pgx · golang-migrate · excelize    |
| Banco    | PostgreSQL 16 (`NUMERIC(14,2)` para valores, `DATE` sem fuso)     |
| Frontend | React + TypeScript + Vite · TanStack Query                        |
| Leitura  | Claude Messages API (visão), saída JSON estruturada               |
| Deploy   | Docker Compose em VPS Hostinger + proxy HTTPS                     |

## Estrutura

```
backend/     API em Go (http, auth, leitura, validacao, pdf, cobranca, exportxlsx, store)
frontend/    React + TypeScript + Vite
deploy/      docker-compose, proxy reverso, scripts
legado/      protótipo HTML + JSONs de referência (SOMENTE LEITURA)
CLAUDE.md    especificação completa: domínio, modelo de dados, regras, API, marcos
```

## Dados de referência (`legado/`)

Base de testes já validada — **não alterar**. Qualquer divergência encontrada deve ser *reportada*,
não corrigida em silêncio.

| Arquivo | Conteúdo |
|---|---|
| `app-cupons-v5.html` | protótipo single-file (OCR Tesseract + IndexedDB) com as regras a portar |
| `cupons-outubro-2026-itens.json` | 18 cupons da Maternidade Célia Câmara — R$ 7.225,77 |
| `dona-iris-outubro-2026.json` | 17 cupons da Dona Íris — 12 confirmados (R$ 11.500,46) + 5 pendentes |
| `cobranca-2026-10-clientes.xlsx` | cobrança de outubro/2026 gerada pelo protótipo (referência do Excel) |

**Critério de aceite da importação do legado:** total confirmado dos dois clientes = **R$ 18.726,23**,
e reimportar os mesmos arquivos não pode duplicar nada. Detalhes em `CLAUDE.md` §10.

## Variáveis de ambiente

```
DATABASE_URL=postgres://...
ANTHROPIC_API_KEY=...     # somente no servidor; nunca no frontend, nunca em log
JWT_SECRET=...
CLAUDE_MODEL=...          # modelo de visão usado na leitura
```

## Desenvolvimento

```bash
cd backend && go vet ./... && go test ./...
cd frontend && npm install && npm run lint && npm test
```

Convenções de trabalho, marcos de execução e o que exige confirmação antes de mudar estão em
[`CLAUDE.md`](CLAUDE.md) §13 e §14.
