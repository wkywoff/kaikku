#!/bin/sh
# Бэкап kaikku в Docker: база (pg_dump) и свои файлы — обложки, фото
# артистов, загруженные релизы и аудио, которое заменил админ (его не скачать
# заново). Остальное скачанное аудио не сохраняется: его можно скачать снова.
# Запускать из любого места, нужен доступ к docker.
#   docker/backup.sh [каталог]   (по умолчанию backups/ в checkout)
# Хранит KEEP_DAYS последних дней (14). Каждую ночь, например из cron:
#   30 3 * * * /path/to/kaikku/docker/backup.sh /var/backups/kaikku
# Восстановить базу:
#   docker compose exec -T postgres pg_restore -U kaikku -d kaikku --clean --if-exists < kaikku-<дата>.dump
set -eu

cd "$(dirname "$0")/.."
DIR="${1:-backups}"
KEEP_DAYS="${KEEP_DAYS:-14}"
stamp=$(date +%F)

mkdir -p "$DIR"
chmod 700 "$DIR"

docker compose exec -T postgres pg_dump -U kaikku -Fc kaikku > "$DIR/kaikku-$stamp.dump.tmp"
mv "$DIR/kaikku-$stamp.dump.tmp" "$DIR/kaikku-$stamp.dump"

docker compose exec -T postgres psql -U kaikku -d kaikku -Atc \
  "SELECT DISTINCT storage_key FROM tracks WHERE audio_pinned AND storage_key <> '' AND storage_key NOT LIKE 'uploads/%'" \
  | docker compose exec -T api sh -c '
      cd /data/storage
      while read -r key; do [ -f "$key" ] && printf "%s\n" "$key"; done > /tmp/kaikku-pinned
      tar czf - $(ls -d covers uploads 2>/dev/null) -T /tmp/kaikku-pinned
      rc=$?; rm -f /tmp/kaikku-pinned; exit $rc' \
  > "$DIR/kaikku-files-$stamp.tar.gz.tmp"
mv "$DIR/kaikku-files-$stamp.tar.gz.tmp" "$DIR/kaikku-files-$stamp.tar.gz"

find "$DIR" -maxdepth 1 -type f -name 'kaikku-*' -mtime +"$KEEP_DAYS" -delete

echo "backup ok: $DIR/kaikku-$stamp.dump ($(du -h "$DIR/kaikku-$stamp.dump" | cut -f1)), files $(du -h "$DIR/kaikku-files-$stamp.tar.gz" | cut -f1)"
