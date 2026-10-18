#!/usr/bin/env bash
# Применяет migrations/NNNN_*.sql, которых ещё нет в schema_migrations.
# Каждый файл — в одной транзакции вместе с записью о нём.
# DATABASE_URL берётся из окружения или из .env в корне checkout.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ -z "${DATABASE_URL:-}" ]; then
  DATABASE_URL="$(grep -E '^DATABASE_URL=' .env | head -1 | cut -d= -f2-)"
fi
export PGOPTIONS="-c client_min_messages=warning"
psql_() { psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -X -q "$@"; }

psql_ -c "CREATE TABLE IF NOT EXISTS schema_migrations (
  version    TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now())"

for f in migrations/[0-9][0-9][0-9][0-9]_*.sql; do
  v="$(basename "$f" .sql)"
  if [ "$(psql_ -At -c "SELECT count(*) FROM schema_migrations WHERE version = '$v'")" = 0 ]; then
    echo "applying $v"
    psql_ -1 -f "$f" -c "INSERT INTO schema_migrations (version) VALUES ('$v')"
  fi
done
echo "migrations up to date"
