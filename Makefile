# Atalhos de desenvolvimento do Cupons ITD.
.DEFAULT_GOAL := ajuda
.PHONY: ajuda subir derrubar logs testar testar-backend testar-frontend vet build limpar deploy backup-agora psql

ajuda: ## Lista os comandos
	@grep -E '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

subir: ## Sobe tudo em desenvolvimento (api + postgres + frontend)
	docker compose up --build

derrubar: ## Derruba os contêineres (mantém o volume do banco)
	docker compose down

logs: ## Acompanha os logs da API
	docker compose logs -f api

testar: testar-backend testar-frontend ## Roda todos os testes

testar-backend: ## go vet + go test do backend
	cd backend && go vet ./... && go test ./...

testar-frontend: ## typecheck + vitest do frontend
	cd frontend && npm run lint && npm test

vet: ## Só o go vet
	cd backend && go vet ./...

build: ## Compila o binário da API localmente
	cd backend && go build -o bin/api ./cmd/api

limpar: ## Remove artefatos locais
	rm -rf backend/bin frontend/dist

deploy: ## Sobe a produção (cupom.fbtax.cloud) — rodar no VPS
	docker compose -f deploy/docker-compose.prod.yml up -d --build

backup-agora: ## Força um backup imediato do Postgres
	docker compose -f deploy/docker-compose.prod.yml exec backup sh /usr/local/bin/backup.sh &
	@echo "backup disparado; veja deploy/backups/"

psql: ## Abre o psql no banco de desenvolvimento
	docker compose exec postgres psql -U cupons -d cupons
