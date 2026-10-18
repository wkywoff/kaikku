# kaikku в Docker: образ app (API, воркер, миграции) и образ web (nginx с
# собранным фронтендом). Собирается и запускается через compose.yaml.

# Без `FROM --platform=$BUILDPLATFORM`: старый сборщик Docker (без плагина
# buildx) этой переменной не знает и падает. arm64 в CI (.github/workflows)
# поэтому собирается целиком под QEMU — дольше, но только на релиз.

# --- фронтенд: в образы попадает только dist ---
FROM node:26-trixie-slim AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
# --ignore-scripts: install-скрипты пакетов npm сборке не нужны.
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# --- API и воркер ---
FROM golang:1.27-trixie AS backend
# Версия для настроек и /api/health: тег релиза (CI) или коммит (compose.yaml).
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X kaikku/internal/api.Version=${VERSION}" -o /out/kaikku-api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kaikku-worker ./cmd/worker

FROM denoland/deno:bin-2.9.7 AS deno

# --- app: API, воркер, миграции ---
FROM debian:trixie-slim AS app
# ffmpeg/ffprobe — звук; python3 — yt-dlp и тёплый помощник API;
# postgresql-client — psql для миграций; curl — проверка здоровья.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl ffmpeg postgresql-client python3 python3-venv \
 && rm -rf /var/lib/apt/lists/*
COPY --from=deno /deno /usr/local/bin/deno

RUN useradd --uid 1000 --create-home --home-dir /home/kaikku kaikku \
 && install -d -o kaikku -g kaikku /data /data/storage /data/staging /data/stream /opt/yt-dlp

# yt-dlp — в своём venv, не от root. В compose этот каталог — volume:
# при первом запуске Docker копирует туда то, что поставлено здесь, а дальше
# API обновляет его сам (docker/update-yt-dlp), без пересборки образа.
USER kaikku
RUN python3 -m venv /opt/yt-dlp \
 && /opt/yt-dlp/bin/pip install --disable-pip-version-check --no-cache-dir 'yt-dlp[default]'
USER root

WORKDIR /app
COPY migrations/ migrations/
COPY --chmod=755 docker/entrypoint.sh /usr/local/bin/entrypoint
COPY --chmod=755 docker/update-yt-dlp /usr/local/bin/update-yt-dlp
COPY --from=backend /out/ /usr/local/bin/
# index.html той же сборки, что у web: API вставляет в него og-теги.
COPY --from=frontend /src/dist/index.html /app/frontend/index.html

ENV PATH=/opt/yt-dlp/bin:/usr/local/bin:/usr/bin:/bin \
    HOME=/home/kaikku \
    LISTEN_ADDR=:8080 \
    STORAGE_DIR=/data/storage \
    STAGING_DIR=/data/staging \
    STREAM_DIR=/data/stream \
    FRONTEND_INDEX=/app/frontend/index.html
USER kaikku
EXPOSE 8080
ENTRYPOINT ["entrypoint"]
CMD ["api"]

# --- web: nginx — сайт, /api/ и файлы по подписанным ссылкам ---
FROM nginx:1.30-alpine AS web
# В шаблоне подставляется только MEDIA_SECRET; $uri и прочее остаётся nginx'у.
ENV NGINX_ENVSUBST_FILTER=^MEDIA_SECRET$
RUN rm -f /etc/nginx/conf.d/default.conf
COPY docker/nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --chmod=755 docker/security-txt.sh /docker-entrypoint.d/40-security-txt.sh
COPY --from=frontend /src/dist/ /usr/share/nginx/html/
