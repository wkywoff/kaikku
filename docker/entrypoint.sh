#!/bin/sh
set -eu

case "${1:-api}" in
  api)
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
