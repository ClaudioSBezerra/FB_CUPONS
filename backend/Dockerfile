# syntax=docker/dockerfile:1

# --- compilação ---
FROM golang:1.26-alpine AS build

WORKDIR /src

# As dependências vêm primeiro: enquanto go.mod/go.sum não mudarem, o Docker
# reaproveita a camada e o build fica em segundos.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

# Roda vet e testes dentro do build: imagem que não passa nos testes do
# domínio (CNPJ, aritmética, cobrança) não chega a ser publicada.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go vet ./... && go test ./...

# CGO desligado gera binário estático, que roda na imagem final mínima.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/api ./cmd/api

# --- execução ---
FROM alpine:3.21

# poppler-utils traz o pdftoppm, usado para rasterizar PDFs que não sejam
# "um JPEG embutido por página". tzdata para datas corretas no log.
RUN apk add --no-cache poppler-utils ca-certificates tzdata \
 && adduser -D -u 10001 cupons

ENV TZ=America/Sao_Paulo

COPY --from=build /bin/api /usr/local/bin/api

USER cupons
EXPOSE 8080

# O healthcheck usa /readyz, que confere o banco além do processo.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/readyz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/api"]
