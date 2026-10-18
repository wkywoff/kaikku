# Development

kaikku runs in Docker (see the [README](../README.md)); this page is for
working on the code. Code comments are mostly in Russian.

| | Version | For |
|---|---|---|
| Go | 1.24+ | API and worker |
| Node.js | 22+ | the frontend |
| PostgreSQL | 17+ | |
| ffmpeg, ffprobe | recent | remuxing, loudness |
| yt-dlp | latest `yt-dlp[default]` | finding and downloading audio |
| Python 3 | 3.9+ | the API's "warm" yt-dlp helper |
| Deno | 2.x, in `PATH` | yt-dlp solves YouTube's challenges with it |

## Running it locally

```bash
# 1. Database
sudo -u postgres createuser -P kaikku          # password: kaikku
sudo -u postgres createdb -O kaikku kaikku
DATABASE_URL=postgres://kaikku:kaikku@localhost:5432/kaikku?sslmode=disable \
  migrations/migrate.sh

# 2. API and worker, each in its own terminal, from the repository root
export DATABASE_URL=postgres://kaikku:kaikku@localhost:5432/kaikku?sslmode=disable
export JWT_SECRET=$(openssl rand -hex 32) MEDIA_SECRET=dev MEDIA_BASE_URL=http://localhost:5173
go run ./cmd/api
go run ./cmd/worker

# 3. Frontend on http://localhost:5173; /api is proxied to :8080
cd frontend && npm ci && npm run dev
```

The first account on an empty database becomes the admin. Files go to
`./data/storage`.

Downloaded tracks and custom covers are served from `/media/` by nginx,
not by the API. To play them in development, serve `data/storage` with
Range support, e.g. nginx with `location /media/ { alias
/path/to/data/storage/; }`, and start the frontend with
`VITE_MEDIA_TARGET=http://localhost:8081 npm run dev`. Tracks that aren't
downloaded yet stream from the API and work without it.

The frontend's own notes: [frontend/README.md](../frontend/README.md).

## Tests

Use separate databases: the tests `TRUNCATE` tables.

```bash
sudo -u postgres createuser -P kaikku_test
sudo -u postgres createdb -O kaikku_test kaikku_test
```

### Go

```bash
export TEST_DATABASE_URL=postgres://kaikku_test:<password>@localhost:5432/kaikku_test?sslmode=disable
DATABASE_URL="$TEST_DATABASE_URL" migrations/migrate.sh
go vet ./... && go test -p 1 -count=1 ./...
```

`-p 1` is required: the `api` and `ingest` packages share one test
database and clean it after each test. Without `TEST_DATABASE_URL` the
database tests are skipped and the rest run as usual.

Covered, among other things:

- signed links against nginx's openssl recipe; PBKDF2 against reference
  vectors;
- the Deezer client on a mock server with real response shapes (errors
  inside HTTP 200, quota retries, the rate limiter, the cache);
- queueing downloads from likes and saved albums on a real database
  (deduplication, album metadata, requeueing failed jobs);
- library, playlists, collaborators, profiles and follows, history, mixes,
  new releases, the artist page cache with admin edits on top;
- sign-up and login (password rules, invites, the pause after failed
  logins);
- picking the YouTube recording on recorded candidate lists;
- streaming a track that isn't downloaded yet (Content-Length and Range
  before the download ends, signed link, hand-over to the worker);
- yt-dlp and the "warm" helper with a fake yt-dlp/python3 (the test binary
  is its own helper process, no network);
- the Subsonic API.

### Frontend

```bash
cd frontend && npm ci && npm test && npx svelte-check && npm run build
```

`npm test` runs `node:test` with fake `Audio`/`AudioContext` for the player
store (`src/lib/stores/player.js`): queue, volume, crossfade timing,
retries after a failed track, streaming a track before it's downloaded.

### Live run against Deezer and YouTube

API on `127.0.0.1:18080` and the worker on a database of its own, files in
`/tmp/kaikku-dev`. Not as root: it downloads from YouTube and runs ffmpeg on
foreign files. Stop it afterwards (`pkill -f /tmp/kaikku-`).

```bash
sudo -u postgres createdb -O kaikku_test kaikku_devlive
export DATABASE_URL=postgres://kaikku_test:<password>@localhost:5432/kaikku_devlive?sslmode=disable
migrations/migrate.sh
go build -o /tmp/kaikku-api ./cmd/api && go build -o /tmp/kaikku-worker ./cmd/worker
mkdir -p /tmp/kaikku-dev/storage /tmp/kaikku-dev/staging
# Secrets go to a file with mode 600: process arguments are visible in ps.
(umask 077; cat > /tmp/kaikku-dev/env <<ENV
DATABASE_URL=$DATABASE_URL
JWT_SECRET=$(openssl rand -hex 32)
MEDIA_SECRET=devsecret
MEDIA_BASE_URL=http://localhost:18081
LISTEN_ADDR=127.0.0.1:18080
STORAGE_DIR=/tmp/kaikku-dev/storage
STAGING_DIR=/tmp/kaikku-dev/staging
ENV
)
for bin in /tmp/kaikku-api /tmp/kaikku-worker; do
  sh -c 'set -a; . /tmp/kaikku-dev/env; exec "$0"' "$bin" &
done
```

Frontend for it: `VITE_BACKEND_TARGET=http://127.0.0.1:18080 npx vite
preview --port 18081` in `frontend/`. Library tracks won't play there (no
nginx with `secure_link`); everything else works, including streaming
tracks that aren't downloaded yet.

Worth checking this way: an album with featured artists and a track with a
Cyrillic title (album, album artist, date, cover, disc; the official or
`- Topic` upload is picked), SIGTERM in the middle of a download (the
worker finishes and exits), `kill -9` in the middle of a download (the job
returns to the queue after a restart), new migrations on a copy of a real
database.

## Layout

```
cmd/api, cmd/worker   entry points
internal/api          HTTP and the Subsonic API; routes are in router.go
internal/auth         PBKDF2-SHA256 passwords, HS256 JWT
internal/deezer       Deezer client: rate limit, two egresses, cache
internal/ytdlp        finding and downloading the recording, the "warm" helper
internal/ingest       worker: download queue, audio, housekeeping
internal/lyrics       lyrics sources and parsers
internal/lastfm       Last.fm scrobbling, bios, tags
internal/media        signed links for nginx
migrations/           NNNN_*.sql and migrate.sh, which applies them in order
docker/               entrypoint, yt-dlp updates, nginx template, backups
.github/workflows     tests (every push and pull request), Docker images (v* tags)
frontend/             Svelte 5 single-page app
```

All `/api/` routes need `Authorization: Bearer <jwt>` except health,
sign-up and login, images, link previews and the stream itself (its link is
signed). `/rest/` has its own sign-in (Subsonic).

## Docker images

`Dockerfile` builds two targets: `app` (API, worker, migrations) and `web`
(nginx with the built frontend). `docker compose up -d --build` builds both
from the checkout; `KAIKKU_BUILD_VERSION=$(git describe --tags --always)`
in front of it puts the version in Settings (otherwise it says `dev`). A
`v*` tag runs `.github/workflows/docker.yml`, which publishes
`ghcr.io/<owner>/kaikku-app` and `kaikku-web` for amd64 and arm64 with the
tag as the version.
