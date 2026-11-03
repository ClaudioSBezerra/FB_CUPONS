# syntax=docker/dockerfile:1

# --- compilação ---
FROM node:22-alpine AS build

WORKDIR /app

COPY package.json package-lock.json* ./
# npm ci exige o lock; sem ele, npm install resolve na hora.
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi

COPY . .

# VITE_API_URL vazio = a API está no mesmo domínio, em /api (produção).
ARG VITE_API_URL=""
ENV VITE_API_URL=$VITE_API_URL

RUN npm run build

# --- execução ---
FROM nginx:1.27-alpine

COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
