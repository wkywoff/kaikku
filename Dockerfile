FROM node:26-trixie-slim AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-trixie AS backend
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X kaikku/internal/api.Version=${VERSION}" -o /out/kaikku-api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kaikku-worker ./cmd/worker

FROM denoland/deno:bin-2.9.8 AS deno

FROM debian:trixie-slim AS app
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates curl ffmpeg postgresql-client python3 python3-venv \
 && rm -rf /var/lib/apt/lists/*
COPY --from=deno /deno /usr/local/bin/deno

RUN useradd --uid 1000 --create-home --home-dir /home/kaikku kaikku \
 && install -d -o kaikku -g kaikku /data /data/storage /data/staging /data/stream /opt/yt-dlp

USER kaikku
RUN python3 -m venv /opt/yt-dlp \
 && /opt/yt-dlp/bin/pip install --disable-pip-version-check --no-cache-dir 'yt-dlp[default]'
USER root

WORKDIR /app
COPY migrations/ migrations/
COPY --chmod=755 docker/entrypoint.sh /usr/local/bin/entrypoint
COPY --chmod=755 docker/update-yt-dlp /usr/local/bin/update-yt-dlp
COPY --from=backend /out/ /usr/local/bin/
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

FROM nginx:1.31-alpine AS web
ENV NGINX_ENVSUBST_FILTER=^MEDIA_SECRET$
RUN rm -f /etc/nginx/conf.d/default.conf
COPY docker/nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --chmod=755 docker/security-txt.sh /docker-entrypoint.d/40-security-txt.sh
COPY --chmod=755 docker/real-ip.sh /docker-entrypoint.d/41-real-ip.sh
COPY --from=frontend /src/dist/ /usr/share/nginx/html/
