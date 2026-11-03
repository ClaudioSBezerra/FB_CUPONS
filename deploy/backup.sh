#!/bin/sh
# Backup diário do Postgres com retenção, rodando como serviço do compose.
#
# Dorme até a hora configurada (HORA_BACKUP, padrão 03h), faz pg_dump
# comprimido em /backups e apaga os arquivos mais antigos que RETENCAO_DIAS.
#
# Restauração:
#   gunzip -c /backups/cupons-2026-11-03.sql.gz | psql -U cupons -d cupons

set -eu

RETENCAO_DIAS="${RETENCAO_DIAS:-14}"
HORA_BACKUP="${HORA_BACKUP:-03}"
DESTINO=/backups

mkdir -p "$DESTINO"

log() { echo "[backup] $(date '+%Y-%m-%d %H:%M:%S') $*"; }

fazer_backup() {
	arquivo="$DESTINO/cupons-$(date '+%Y-%m-%d-%H%M').sql.gz"
	log "iniciando dump em $arquivo"

	# --clean deixa o dump restaurável sobre um banco existente.
	if pg_dump --clean --if-exists --no-owner --no-privileges | gzip -9 > "$arquivo.parcial"; then
		mv "$arquivo.parcial" "$arquivo"
		log "concluído: $(du -h "$arquivo" | cut -f1)"
	else
		rm -f "$arquivo.parcial"
		log "ERRO: o pg_dump falhou — nenhum backup foi gravado"
		return 1
	fi

	# Confere que o arquivo não saiu vazio ou truncado.
	if ! gzip -t "$arquivo"; then
		log "ERRO: $arquivo está corrompido; removendo"
		rm -f "$arquivo"
		return 1
	fi

	antigos=$(find "$DESTINO" -name 'cupons-*.sql.gz' -mtime "+$RETENCAO_DIAS" -print)
	if [ -n "$antigos" ]; then
		log "removendo backups com mais de $RETENCAO_DIAS dias"
		echo "$antigos" | while read -r velho; do
			[ -n "$velho" ] && rm -f "$velho" && log "  removido $(basename "$velho")"
		done
	fi
}

log "serviço de backup ativo (diário às ${HORA_BACKUP}h, retenção de ${RETENCAO_DIAS} dias)"

# Um backup imediato na subida: se a configuração estiver errada, o operador
# descobre agora e não só na primeira madrugada.
fazer_backup || log "o backup inicial falhou; o serviço continua e tentará no horário"

while true; do
	agora_h=$(date '+%H')
	agora_m=$(date '+%M')
	# Segundos até a próxima ocorrência de HORA_BACKUP:00.
	alvo=$(( (10#$HORA_BACKUP * 3600) - (10#$agora_h * 3600 + 10#$agora_m * 60) ))
	[ "$alvo" -le 0 ] && alvo=$(( alvo + 86400 ))

	log "próximo backup em $(( alvo / 3600 ))h$(( (alvo % 3600) / 60 ))min"
	sleep "$alvo"

	fazer_backup || log "o backup falhou; tentará novamente amanhã"
done
