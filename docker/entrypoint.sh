#!/bin/sh
# Точка входа образа app: api | worker | migrate | любая команда.
set -eu

case "${1:-api}" in
  api)
    # yt-dlp обновляет только API (у воркера тот же volume, только чтение):
    # при старте и потом раз в YTDLP_UPDATE_HOURS часов. Если вышла новая
    # версия, API перезапускается (restart: unless-stopped в compose), чтобы
    # тёплый помощник подхватил её; воркер запускает yt-dlp на каждый трек
    # заново и подхватывает сам.
    update-yt-dlp || echo "update-yt-dlp failed, carrying on with the installed version" >&2
    hours="${YTDLP_UPDATE_HOURS:-24}"
    if [ "$hours" -gt 0 ] 2>/dev/null; then
      (
        while sleep "$((hours * 3600))"; do
          update-yt-dlp --restart || true
        done
      ) &
    fi
    exec kaikku-api
    ;;
  worker)
    exec kaikku-worker
    ;;
  migrate)
    exec /app/migrations/migrate.sh
    ;;
  *)
    exec "$@"
    ;;
esac
