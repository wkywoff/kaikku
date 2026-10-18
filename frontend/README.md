# kaikku — frontend

Svelte 5 (legacy syntax: `$:`, `on:click`) + Vite. A small History API
router of its own (`src/lib/route.js`, `src/lib/stores/nav.js`): every
screen has an address, so back/forward, mouse buttons, gestures and links
work as on any website, and the scroll position comes back
(`scrollMemory`). List rows are real links (`use:link`): a plain click stays
in the app, Ctrl/middle click opens a new tab. nginx serves `index.html` for
any unknown path, so direct links open.

```
/                                   home: recent, mixes, new releases, suggestions, friends' playlists
/library                            library: liked songs, playlists, albums, artists, import
/search?q=                          search
/collection/tracks                  liked songs
/playlist/<uuid>                    playlist
/mix/<id>                           mix: on-repeat, forgotten, tag-<genre>
/genre/<slug>                       genre (Last.fm tags)
/artist/<id>                        artist: top, discography, similar, follow, radio
/artist/<id>/discography/<filter>   full discography (all | album | ep | single)
/album/<id>                         album (?track=<id> highlights a track)
/user/<name>                        profile: follow, public playlists
/upload, /upload/<id>               own releases: list and upload dialog
/admin/missing                      admin: tracks that failed to download
/wrapped[/<year>]                   year in review
/settings                           settings (/lastfm?token= comes back here)
```

Artist, album and track IDs are Deezer numbers or `k…` for uploaded releases.

How it fits together:

- **Player**: `src/lib/stores/player.js`, mounted in `App.svelte` outside
  the screens. Two `<audio>` elements go through `createMediaElementSource`
  → their own `GainNode` → a shared `destination`: crossfade or gapless,
  volume and normalisation (`gain_db`). Not `decodeAudioData`: it keeps the
  whole track uncompressed in memory and crashes mobile tabs. On iOS audio
  plays straight from `<audio>` (`directAudio`), without Web Audio. The
  queue, the current track and the position survive a reload (localStorage);
  signing out (`src/lib/signout.js`) wipes them and reloads. A track that
  fails shows a message; if it started by itself rather than from a click,
  the player moves on (at most three in a row). Autoplay (off by default)
  continues a finished queue with the artist's radio. Media Session handles
  the lock screen.
- **Where it plays from**: each queue item has a `context` (album,
  playlist, artist, liked songs, mix). History is recorded from it (after
  30 s), and Play buttons in headers and cards know whether they are playing
  (`src/lib/library.js`: `playingContext`, `playFrom`).
- **Track rows** (`src/lib/rows.js`) come from Deezer responses; downloaded
  tracks play from the server, the rest stream while the server downloads
  them (`library.js` → `playRows`).
- **Library** (`src/lib/stores/collection.js`): likes, saved albums,
  followed artists, playlists; changes apply at once and roll back on error.
- **Track menu** (`src/lib/trackMenu.js`, `ContextMenu.svelte`): right click
  or "…": playlist, like, queue, radio, sleep timer, links, edit (admin).
- **Admin mode**: a switch in the account menu (`stores/me.js`) that shows
  edit buttons for titles, dates, types and covers (`EditDialog.svelte`).
- **Colours**: pages and the player take the cover's colour
  (`src/lib/color.js`, `stores/theme.js`); Deezer images go through
  `/api/img` at the needed size (`src/lib/imageproxy.js`).
- **Invites**: in the admin's account menu; the one-time link
  `/?invite=<code>` opens sign-up.

## Tests

`npm test`: `node:test` with fake `Audio`/`AudioContext` (jsdom has no Web
Audio): queue, volume, crossfade and gapless, retry on error, saved queue,
autoplay, history; routes, colours, formats. See also
[docs/development.md](../docs/development.md).

## Development

```bash
npm install
npm run dev
```

The Vite proxy (`vite.config.js`) sends `/api` and `/media` to
`VITE_BACKEND_TARGET` (default `http://localhost:8080`), so local
development needs no CORS.

`/media/` (downloaded tracks, custom covers) is served by nginx straight
from disk, not by the Go process; without nginx next to it, those don't
load in dev mode (see the note in
[docs/development.md](../docs/development.md)). Expected, not a bug.

## Build

```bash
npm run build
```

Static files go to `dist/`; nginx serves them directly. In Docker they are
baked into the `web` image.

## Not verified

Playback, gapless and the lock screen on iPhone (Safari) have only been
checked on desktop and in browser emulation.

## Installing as an app (PWA)

`public/manifest.json`, `src/sw.js` (the build writes it to `/sw.js` with
the build id in the cache name, see `vite.config.js`; registered in
`src/main.js`, production builds only) and the icons. The service worker
caches the app shell only: music and API calls always go to the network.

The mark is a K whose arms are sound waves. `public/icon.svg` is the tab icon and the logo in the interface
(one wave per arm, so it reads at 16 px); `brand/icon-app.svg` is the app
icon with rounded corners, `brand/icon-fullbleed.svg` fills the square
(Android maskable, iOS apple-touch-icon: they crop it themselves). PNGs are
rendered with ffmpeg built with librsvg:

```bash
render() { # svg, size, png
  sed "s/width=\"512\" height=\"512\">/width=\"$2\" height=\"$2\">/" "$1" > /tmp/icon.svg
  ffmpeg -nostdin -loglevel error -y -i /tmp/icon.svg -frames:v 1 "$3"
}
render brand/icon-app.svg 192 public/icon-192.png
render brand/icon-app.svg 512 public/icon-512.png
render brand/icon-fullbleed.svg 512 public/icon-maskable-512.png
render brand/icon-fullbleed.svg 180 public/apple-touch-icon.png
```
