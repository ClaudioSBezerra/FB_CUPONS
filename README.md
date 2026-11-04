# Cupons ITD

Sistema web para ler **cupons fiscais térmicos** emitidos pela ITD Padaria e Confeitaria Eireli – ME
(STRAVAGANZA, CNPJ 29.220.487/0001-08) e gerar a **cobrança mensal em Excel** por cliente e produto.

A leitura é feita pela **API do Claude (visão)** no backend — o OCR Tesseract do protótipo falhava em
impressão térmica apagada. Toda dúvida vai para revisão humana; nada é "chutado" em silêncio.

> **Sistema crítico:** não pode haver erro de CNPJ, valor ou quantidade.

Produção: **https://cupom.fbtax.cloud**

---

## Como funciona a importação

```
PDF do escâner do iPhone
        │
        ├─ 1 JPEG embutido por página  ──► extraído sem recompressão (traço fino preservado)
        └─ PDF vetorial/misto          ──► rasterizado com pdftoppm a 300 dpi
        │
        ▼
   SHA-256 por imagem ──► já importada? então nem gasta leitura
        │
        ▼
   Claude (visão) + schema JSON  ──► campo ilegível vem como null, nunca como palpite
        │
        ▼
   Validações (CNPJ, aritmética, catálogo, datas)
        │
        ▼
   Cupom gravado como PENDENTE  ──► Revisão humana  ──► Confirmado  ──► entra na cobrança
```

Uma foto solta de celular é **um** cupom: fotos altas nunca são fatiadas em vários.

## Stack

| Camada   | Tecnologia                                                        |
|----------|-------------------------------------------------------------------|
| Backend  | Go 1.26 · `net/http` + chi · pgx · golang-migrate · excelize      |
| Banco    | PostgreSQL 16 — `NUMERIC(14,2)` para valores, `DATE` sem fuso     |
| Frontend | React 18 + TypeScript + Vite · TanStack Query                     |
| Leitura  | Claude Messages API (visão), saída JSON estruturada                |
| Deploy   | Docker Compose + Caddy (HTTPS automático) no VPS Hostinger        |

**Valores e quantidades nunca usam `float64`.** `dominio.Valor` é inteiro em centavos e
`dominio.Qtd` é inteiro em milésimos; a fronteira com o Postgres é texto convertido por
`::numeric`. Arredondamento binário em um sistema de cobrança produz centavos fantasmas.

## Estrutura

```
backend/
  cmd/api/main.go          sobe a API, aplica migrations, cria o admin inicial
  internal/
    dominio/               Valor, Qtd, Data, Cupom, Item — tipos exatos
    validacao/             CNPJ, aritmética, catálogo, datas (puro, sem banco nem IA)
    leitura/               cliente Claude (visão), prompt, schema, leitor gravado
    imagem/                normalização para os limites da API de visão
    pdf/                   PDF → uma imagem por página
    importacao/            orquestra o lote e o importador do legado
    store/                 PostgreSQL (pgx)
    cobranca/ exportxlsx/  agregação e planilha
    http/ auth/ config/    API REST, sessão em cookie httpOnly, env
  migrations/              SQL versionado, embutido no binário
frontend/                  Importar · Revisão · Cupons · Cobrança · Cadastros
deploy/                    compose de produção, Caddyfile, backup
legado/                    protótipo HTML + JSONs de referência (SOMENTE LEITURA)
CLAUDE.md                  especificação completa
```

## Desenvolvimento

```bash
cp .env.example .env     # preencha POSTGRES_PASSWORD, JWT_SECRET, ADMIN_*, ANTHROPIC_API_KEY
make subir               # api:8080 + postgres + frontend:5173
make testar              # go vet, go test, typecheck e vitest
```

Sem `ANTHROPIC_API_KEY` o sistema sobe normalmente — revisão manual, cobrança e Excel
funcionam; só a importação automática fica indisponível.

## Deploy

Há três compose, e a escolha importa:

| Arquivo | Quando usar |
|---|---|
| `docker-compose.yml` | desenvolvimento local |
| `docker-compose.coolify.yml` | **Coolify** — nenhum serviço publica porta; quem roteia é o proxy do Coolify |
| `deploy/docker-compose.prod.yml` | VPS com Docker puro — sobe um Caddy próprio nas portas 80/443 |

Nos três, o navegador fala só com o frontend: o nginx dele serve a SPA e repassa
`/api` para a API pela rede interna. Um caminho só, cookie de sessão first-party
e sem CORS.

> **A aplicação não tem URL de API configurável, de propósito.** Ela chama
> `/api` na mesma origem, sempre. Uma URL absoluta embutida em tempo de build
> viaja dentro do JS, e se estiver errada o navegador de *quem acessa* tenta
> falar com outro endereço. Foi o que aconteceu: o Coolify injeta toda variável
> do painel como `--build-arg`, um `VITE_API_URL=http://localhost:8080` entrou
> no bundle e o site subiu com `Failed to fetch` em todas as chamadas. Se essa
> variável existir no painel, apague — hoje ela é inerte, mas confunde.

### No Coolify

Não use o `docker-compose.prod.yml` aqui: o Caddy dele disputaria as portas
80/443 com o proxy do Coolify.

1. **Novo recurso → Docker Compose** (baseado em Git).
2. Em **Docker Compose Location**, troque para **`/docker-compose.coolify.yml`**.
   O padrão é `/docker-compose.yml`, que é o de desenvolvimento e publica porta
   no host — é o que causa `Bind for 0.0.0.0:8080 failed: port is already allocated`.
3. Em **Environment Variables**, cole o bloco de
   [`coolify-env-template.txt`](coolify-env-template.txt) e substitua o que está
   em MAIÚSCULAS.
4. Em **Domains**, no serviço **frontend**, informe a URL **com o esquema**:

   ```
   https://cupom.fbtax.cloud
   ```

   O serviço **api** não recebe domínio — o nginx do frontend repassa `/api`
   pela rede interna.
5. Deploy.

> **O `https://` não é opcional.** Sem o esquema, o Coolify trata o valor como
> *caminho* e gera a regra
> ``Host(``) && PathPrefix(`cupom.fbtax.cloud`)`` — host vazio, que não casa com
> requisição nenhuma. O sintoma é 404 do Traefik mesmo batendo direto no IP com
> o cabeçalho `Host` correto, e nenhum roteador `https`, logo nenhum
> certificado. Diagnóstico no servidor:
>
> ```bash
> docker inspect $(docker ps -qf name=frontend-<uuid>) \
>   --format '{{json .Config.Labels}}' | tr ',' '\n' | grep -i traefik
> ```

> **`503 no available server` vindo do Traefik não se resolve mexendo em rede.**
> O frontend e o `coolify-proxy` já compartilham a rede do recurso — o Coolify
> conecta o proxy sozinho. O 503 vem do rótulo com host vazio, e a saída é não
> depender do Traefik: o túnel entrega direto no contêiner.

> **O tráfego entra por Cloudflare Tunnel, não pelo proxy do Coolify.** Neste
> servidor o Coolify gera rótulos de Traefik malformados — host vazio e o
> domínio como *caminho*:
>
> ```
> rule: Host(``) && PathPrefix(`cupom.fbtax.cloud`)
> ```
>
> E isso vale para **todos** os recursos, inclusive os que funcionam: o
> `smartpick.fbtax.cloud` tem exatamente o mesmo rótulo defeituoso e responde
> 200. O que o faz funcionar é o `cloudflared`. Sete recursos deste servidor
> usam túnel (FB_SMARTPICK, FB_FAROL, FB_APU02/04/05, FB_AUDITORIA, STOCKFLOW),
> e é o padrão que este projeto segue.
>
> Consequências práticas: o campo **Domains** do Coolify é irrelevante, e o
> registro DNS é criado e mantido pelo próprio túnel — registro A manual deve
> ser removido para não disputar o nome.

#### O que impede a API de subir, e o que só avisa

Só duas coisas são fatais, porque sem elas a API não tem como servir:

| Fatal | Por quê |
|---|---|
| banco inacessível ou migration falhando | não há onde ler nem gravar |
| `JWT_SECRET` ausente ou com menos de 32 caracteres | sessão forjável |

Todo o resto **avisa no log e a API sobe**:

| Situação | O que acontece |
|---|---|
| `ANTHROPIC_API_KEY` ausente | aba Importar desativada; revisão, cobrança e Excel funcionam |
| `ADMIN_SENHA` ausente ou curta | a API **sorteia** uma senha forte, cria o admin e anuncia no log: `administrador criado com SENHA SORTEADA` |
| `ADMIN_EMAIL` ausente | nenhum usuário é criado, com aviso de que ninguém poderá entrar |
| já existe usuário | o admin não é recriado, com aviso de que trocar `ADMIN_SENHA` não altera quem existe |

E a validação é **acumulada**: um único deploy lista todos os problemas de
configuração numerados, em vez de morrer num por reinício.

> **Por que esse arquivo fica na raiz e não em `deploy/`:** o Coolify roda o
> compose com `--project-directory` na raiz do repositório, e o Docker Compose
> resolve `build.context` contra o *project directory*, não contra a pasta do
> arquivo. Em `deploy/`, um `context: ../backend` apontaria para fora do
> repositório e o build falharia.

> **O admin é criado uma única vez**, na primeira subida com a tabela de usuários
> vazia. Mudar `ADMIN_SENHA` depois não altera o usuário existente.

> **`POSTGRES_PASSWORD` também só vale na primeira inicialização.** A senha fica
> gravada no papel do banco, dentro do volume. Se um deploy anterior já criou o
> volume, trocar a variável faz a API mandar a senha nova para um banco que
> guarda a antiga, e o log mostra
> `password authentication failed for user "cupons"`. Saídas:
>
> - **volume ainda sem dados** (o caso de um deploy que nunca chegou a aplicar as
>   migrations): apague o volume do Postgres na aba **Storages** do recurso e
>   redeploy;
> - **volume com dados**: troque a senha dentro do banco, sem apagar nada —
>   ```bash
>   docker exec -it <container-do-postgres> \
>     psql -U cupons -d cupons -c "ALTER USER cupons WITH PASSWORD 'a-senha-do-painel';"
>   ```
>   (funciona sem senha porque a conexão pelo socket local é confiável na imagem
>   oficial), e então redeploy.
>
> A API detecta esse caso e imprime essa explicação no log em vez de só repetir
> o erro do driver.

### No VPS com Docker puro

1. **DNS** (antes de subir, para o Let's Encrypt validar):

   | Tipo | Nome  | Valor        |
   |------|-------|--------------|
   | A    | cupom | `IP do VPS`  |

2. **No VPS** (portas 80 e 443 abertas no firewall):

   ```bash
   git clone https://github.com/ClaudioSBezerra/FB_CUPONS.git
   cd FB_CUPONS
   cp .env.example .env
   nano .env            # AMBIENTE=producao e TODOS os segredos
   docker compose -f deploy/docker-compose.prod.yml up -d --build
   ```

3. **Conferir:**

   ```bash
   curl https://cupom.fbtax.cloud/readyz          # {"ok":true,"banco":"ok"}
   docker compose -f deploy/docker-compose.prod.yml logs -f api
   ```

O Caddy emite e renova o certificado sozinho. Para testar sem gastar o limite de emissão
do Let's Encrypt, descomente `acme_ca` (staging) em `deploy/caddy/Caddyfile`.

Em produção o Postgres **não** publica porta, a API fica só na rede interna, e o frontend e
a API respondem no mesmo domínio (`/api`) — o cookie de sessão é first-party, sem CORS.

**Rollback:** `git checkout <tag-anterior> && docker compose -f deploy/docker-compose.prod.yml up -d --build`.
As migrations são só de avanço; para voltar o esquema, restaure o backup.

**Backup:** o serviço `backup` faz `pg_dump` comprimido diário às 03h em `deploy/backups/`,
com retenção de 14 dias, e um backup imediato na subida (se a configuração estiver errada,
você descobre na hora). Restaurar:

```bash
gunzip -c deploy/backups/cupons-2026-11-03-0300.sql.gz | \
  docker compose -f deploy/docker-compose.prod.yml exec -T postgres psql -U cupons -d cupons
```

## Variáveis de ambiente

Todas em [`.env.example`](.env.example), com explicação. As que mais importam:

| Variável | Para que serve |
|---|---|
| `ANTHROPIC_API_KEY` | credencial da leitura. **Só no servidor** — nunca vai ao frontend nem ao log |
| `ANTHROPIC_BASE_URL` | vazio = `api.anthropic.com`. Preencha para um backend compatível com a API Messages. Chave de gateway contra `api.anthropic.com` volta `401 invalid x-api-key` |
| `LEITURA_RECURSOS_ANTHROPIC` | cache de prompt, raciocínio adaptativo e saída estruturada por schema. Vazio decide pelo endereço: ligado em `api.anthropic.com`, desligado fora dela |
| `CLAUDE_MODEL` | modelo da leitura normal (padrão `claude-opus-5`) |
| `CLAUDE_MODEL_FORTE` | modelo do botão "reler com modelo mais forte" (padrão `claude-fable-5`) |
| `CLAUDE_ESFORCO` | `low`…`max`. Cupom apagado melhora com esforço maior, e custa mais |
| `LEITURA_LADO_MAX` | lado maior enviado à API (1568 = ponto em que a API pararia de ganhar resolução; `0` envia sem redimensionar) |
| `LIMITE_LEITURAS_DIA` | teto diário de chamadas, para controle de custo. `0` desliga |
| `LEITURA_CONCORRENCIA` | páginas lidas em paralelo (acima de 3 cresce o risco de 429) |

## Dados de referência (`legado/`)

Base de testes já validada — **não alterar**. Divergência encontrada é *reportada*, não
corrigida em silêncio.

| Arquivo | Conteúdo |
|---|---|
| `app-cupons-v5.html` | protótipo single-file (OCR Tesseract + IndexedDB) |
| `cupons-outubro-2026-itens.json` | 18 cupons da Maternidade Célia Câmara — R$ 7.225,77 |
| `dona-iris-outubro-2026.json` | 17 cupons da Dona Íris — 12 confirmados (R$ 11.500,46) + 5 pendentes |
| `cobranca-2026-10-clientes.xlsx` | cobrança de outubro/2026 gerada pelo protótipo |

Critério de aceite: total confirmado dos dois clientes = **R$ 18.726,23** (CLAUDE.md §10),
e reimportar não duplica nada.

```bash
# importa os dois JSONs e devolve a conferência por cliente
curl -b cookies.txt -F arquivos=@legado/cupons-outubro-2026-itens.json \
                    -F arquivos=@legado/dona-iris-outubro-2026.json \
     https://cupom.fbtax.cloud/api/legado/importar
```

A agregação da aba *Cobrança* do xlsx de referência foi reproduzida a partir dos dois JSONs:
31 chaves (cliente + produto + unidade), idênticas em quantidade, nº de cupons e valor.

## Trocar o provedor da leitura

A leitura fala a API Messages, então qualquer backend compatível serve — mas
**o modelo precisa enxergar imagem**. Um modelo só de texto não falha de forma
óbvia: ele responde como se não houvesse foto, e a cobrança sai errada.

| Provedor | Configuração | Visão |
|---|---|---|
| Anthropic | `ANTHROPIC_BASE_URL` vazio, `CLAUDE_MODEL=claude-opus-5` | sim |
| DeepSeek | `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic`, `CLAUDE_MODEL=deepseek-flash` | sim, em `deepseek-flash`; `deepseek-v4-pro` **não** |
| OmniRoute (`combo_omniroute`) | — | **não** — responde `no vision-capable provider connected` |

Fora de `api.anthropic.com`, os recursos exclusivos (cache de prompt,
raciocínio adaptativo, saída estruturada por schema) são **desligados
automaticamente**, porque um backend compatível costuma recusá-los. Sem a saída
estruturada o JSON passa a depender da instrução do prompt — o parser tolera
cerca de markdown e texto em volta, mas a garantia de schema se perde. Para
forçar, `LEITURA_RECURSOS_ANTHROPIC=1`.

`CLAUDE_MODEL_FORTE` também precisa ter visão: é o modelo do botão "reler com
modelo mais forte" na Revisão.

## A importação é assíncrona

`POST /api/importacoes` responde **202** com o id do lote e a leitura segue no
servidor. A tela acompanha por `GET /api/importacoes/{id}`, que publica
progresso página a página.

Não é preferência de arquitetura, é necessidade: ler 18 páginas pela API de
visão leva minutos, e o proxy da Cloudflare corta a requisição por volta de
**100 segundos**. Quando cortava, o contexto da requisição era cancelado e
**todas** as leituras em andamento morriam com `context canceled` — um lote
inteiro perdido depois de já ter gasto as chamadas pagas. O trabalho de fundo
usa `context.WithoutCancel`, então o cancelamento do HTTP não o alcança.

Efeito colateral útil: dá para fechar a aba durante a leitura.

## Zerar o movimento para testar

`POST /api/admin/zerar-movimento` com `{"confirmacao":"APAGAR"}`, restrito a
admin, ou o botão em **Cadastros**. Apaga cupons, itens, fotos, importações e o
log de leituras; **preserva** clientes, produtos (inclusive os "a confirmar") e
usuários.

As fotos entram na limpeza de propósito: a importação deduplica imagens por
SHA-256, então sem removê-las o mesmo PDF seria recusado como "já importada" e
o teste não exercitaria a leitura.

## Onde ver o que deu errado

Toda falha de leitura aparece em **dois** lugares, de propósito:

| Lugar | Para quê |
|---|---|
| log da aplicação (`stdout`) | diagnóstico imediato: `falha ao ler cupom` com arquivo, página, modelo e o erro; `importação iniciada` e `importação concluída` com o resumo |
| tabela `leituras_log` | auditoria e custo: uma linha por chamada ao Claude, com tokens, duração e a resposta bruta |

A linha de subida `leitura via Claude habilitada` traz o campo `endereco`, que
diz para onde as requisições vão — é a informação que faltava quando uma chave
de gateway foi usada contra `api.anthropic.com`.

Consultar as últimas falhas direto no banco:

```sql
SELECT criado_em, modelo, left(erro, 300)
  FROM leituras_log WHERE erro IS NOT NULL
 ORDER BY criado_em DESC LIMIT 10;
```

## Divergências do CLAUDE.md, e por quê

Decisões conscientes, documentadas no código onde valem:

1. **`produtos` tem `id` próprio e `codigo` nullable**, não `codigo TEXT PRIMARY KEY` (§4).
   Nos dados reais há **dois** produtos distintos sem código legível (PÃO DOCE 25G da
   Maternidade e o 3º item do cupom 1342014 da Dona Íris) — com o código como chave
   primária, os dois não cabem. A unicidade do código continua, por índice parcial.

2. **`UNIQUE(numero, cnpj_cliente)` é índice parcial** `WHERE numero <> ''`. O número fica
   vazio quando ilegível, e dois cupons ilegíveis do mesmo cliente não são o mesmo cupom.

3. **Status ausente no JSON do legado = confirmado.** É o que reproduz exatamente os totais
   do critério de aceite — no JSON da Maternidade nenhum cupom traz o campo `status`.

## Pendências conhecidas

- **Código `1645` em conflito** no CLAUDE.md: consta no catálogo como `ROSCAS 50G` (é o que
  a Maternidade usa) e também na lista de códigos ilegíveis da Dona Íris, onde o aviso do
  cupom 1180221 diz "pão francês integral". Um código, dois produtos — precisa de decisão.
- **Cupom 1180221**: o JSON traz `total = 0`; o xlsx de referência traz R$ 1.106,50 (a soma
  dos itens). Qual vale na importação?
- **Teste de integração automatizado** (`go test` contra um Postgres de container) ainda
  não escrito. O fluxo foi validado manualmente contra banco real — ver abaixo — mas isso
  não roda em CI.
- Abas de Admin (usuários e log de custo) existem na API, não na interface.

## Validação contra banco real (2026-11-03)

Executado com `docker compose up` e os dois JSONs do legado:

| Verificação | Resultado |
|---|---|
| Migrations 1 e 2 | aplicadas, versão 2, não *dirty* |
| Seeds | 3 clientes, 22 produtos do catálogo + 3 a confirmar |
| Maternidade Célia Câmara | 18 cupons, **R$ 7.225,77** |
| Dona Íris | 12 confirmados **R$ 11.500,46** + 5 pendentes |
| Total confirmado | **R$ 18.726,23** |
| Reimportar os mesmos arquivos | 18 ignorados, 0 duplicados |
| Cobrança vs. xlsx de referência | 31 chaves, idênticas em qtd e nº de cupons; em valor, iguais a menos de um resíduo de float no **arquivo de referência** (ver nota abaixo) |
| Excel gerado | subtotais e total exatos; data serial 46311 = 16/10/2026, sem desvio de fuso |
| Topologia do Coolify (sem porta publicada) | API sobe em `producao`, nginx serve a SPA e repassa `/api` e `/readyz` pela rede interna |
| Subida sem `ANTHROPIC_API_KEY` | API sobe com aviso; `/me` devolve `leitura_habilitada: false` e `POST /importacoes` responde 503 explicando — sem 500 nem tela quebrada |
| Senha do banco com espaço, `@`, `/` e `#` | sobe normalmente: a API monta a DSN com percent-encoding a partir das partes |
| `ADMIN_SENHA=123` (curta) | API sobe, sorteia senha forte, cria o admin e anuncia no log; login com essa senha devolve 200 e cookie `HttpOnly; Secure` |
| Bundle sem endereço absoluto | `grep` no JS gerado só acha `w3.org` (namespace de SVG) e `reactjs.org` (mensagem de erro) |
| Ensaio completo em modo `producao` | login, importação dos dois JSONs, reimportação sem duplicar (12 ignorados, 5 pendentes atualizados), cobrança e download do Excel — tudo pela rede interna, com `Authorization: Bearer` no lugar do cookie `Secure` |

**Nota sobre a única diferença de valor:** o xlsx do protótipo traz
`99.90000000000001` para 10 × PÃO DE FORMA 500G a R$ 9,99 — resíduo de float64 do
JavaScript. A API devolve `99.90` exatos. Com tolerância de R$ 0,01 não há
nenhuma divergência nas 31 chaves, e onde há diferença é o sistema novo que está
certo. É exatamente o que `dominio.Valor` em centavos inteiros existe para evitar.

Três bugs foram encontrados e corrigidos nesse processo — todos invisíveis sem um
Postgres de verdade:

1. `CREATE INDEX ... date_trunc('month', data)` era recusado com
   `functions in index expression must be marked IMMUTABLE`: com coluna `date` ou
   `timestamptz` o Postgres escolhe a variante `timestamptz` de `date_trunc()`, que é
   `STABLE` por depender do fuso. Os índices passaram a ser sobre as colunas e as
   consultas filtram competência por intervalo semiaberto — além de voltar a usar índice,
   o resultado deixou de depender de `TimeZone`.
2. `incertos` e `avisos` nulos violavam `TEXT[] NOT NULL`. Cupom sem aviso é o caso
   normal, então **só os cupons com problema eram gravados** — o inverso do desejado.
3. O nginx do frontend recusaria o upload com 413: o limite padrão de corpo é 1 MB,
   contra os 50 MB de um lote de cupons.
4. A `DATABASE_URL` era montada colando a senha por interpolação de string no
   compose, o que quebrava com senha contendo espaço, `@`, `/`, `:`, `?` ou `#` —
   e o erro só aparecia dentro do driver, longe de onde se configura. Agora o
   compose passa as partes (`DB_HOST`, `DB_USUARIO`, `DB_SENHA`, `DB_NOME`) e a
   API monta a DSN com `url.UserPassword`, que faz o escape. Uma `DATABASE_URL`
   pronta continua tendo precedência, e é validada na subida com mensagem que
   diz o que fazer.

## Convenções

Marcos de execução e o que exige confirmação antes de mudar estão em
[`CLAUDE.md`](CLAUDE.md) §13 e §14. Em resumo: `make testar` antes de dar algo como pronto,
nada em `legado/` é alterado, e nenhuma validação é relaxada para fazer um teste passar.
