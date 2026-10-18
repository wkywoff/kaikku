# Configuration

All settings are environment variables in `.env`, which Docker Compose
reads from the repository folder. Start from
[`.env.example`](../.env.example).

## Required

| Variable | What it is |
|---|---|
| `JWT_SECRET` | Signs logins and encrypts app passwords. `openssl rand -hex 32`. Without it a random one is made on every start and everyone is signed out on restart; changing it turns app passwords off. |
| `MEDIA_SECRET` | Signs links to audio files; nginx checks them. `openssl rand -hex 32`. Docker passes it to nginx too. |

## Common

| Variable | Default | What it does |
|---|---|---|
| `MEDIA_BASE_URL` | `http://localhost:8080` | The site's public address, e.g. `https://music.example.com`, no trailing slash. Used for full links in link previews and for artwork in Subsonic apps. Tracks play without it. |
| `LASTFM_API_KEY`, `LASTFM_API_SECRET` | off | A Last.fm app ([create one](https://www.last.fm/api/account/create), callback `<site>/lastfm`). Turns on scrobbling, genres and genre pages, genre mixes and Last.fm artist bios. |
| `AUDIO_KEEP_DAYS` | `3` | Days (1–14) to keep audio nobody plays. It's downloaded again on the next play; uploads and files an admin replaced are kept. |
| `WORKER_CONCURRENCY` | `3` | Downloads at the same time. |
| `DEEZER_PROXY` | off | A second way to Deezer, e.g. Cloudflare WARP as a SOCKS5 proxy. Another IP doubles Deezer's per-IP limit. A proxy on the host is `socks5://host.docker.internal:40000`; it must listen on the Docker bridge (172.17.0.1) or 0.0.0.0, not only on 127.0.0.1. |
| `MEDIA_TTL_SECONDS` | `600` | How long a link to an audio file works. The player renews it. |

## Docker

| Variable | Default | What it does |
|---|---|---|
| `HTTP_BIND` | `127.0.0.1` | Where the site listens on the host: `127.0.0.1` for this machine only (behind a reverse proxy), `0.0.0.0` for your network too. |
| `HTTP_PORT` | `8080` | The port on the host. |
| `POSTGRES_PASSWORD` | `kaikku` | The database password inside Docker's network (the database isn't exposed). It's set when the database is created: changing it in `.env` later locks the app out, so change it in Postgres first (`ALTER USER kaikku PASSWORD '…'`). |
| `KAIKKU_VERSION` | `latest` | Image version from ghcr.io: `latest` or a release tag such as `v1.2.0`. |
| `YTDLP_UPDATE_HOURS` | `24` | How often the API checks for a new yt-dlp; `0` checks on start only. |

## Outside Docker

Only for [development](development.md): Docker sets these itself.

| Variable | Default | What it does |
|---|---|---|
| `DATABASE_URL` | `postgres://kaikku:kaikku@localhost:5432/kaikku?sslmode=disable` | PostgreSQL connection. |
| `LISTEN_ADDR` | `:8080` | Where the API listens. |
| `STORAGE_DIR` | `./data/storage` | Audio, covers and uploads. |
| `STAGING_DIR` | `./data/staging` | Unfinished downloads. |
| `STREAM_DIR` | `<STAGING_DIR>/../stream` | Streams of tracks that aren't downloaded yet. |
| `FRONTEND_INDEX` | off | Path to the built `index.html` (`frontend/dist/index.html`); turns on link previews. |
