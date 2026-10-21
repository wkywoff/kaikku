# How it works

```
Browser / app ──HTTP(S)──▶ nginx
                           ├─ /              the web app (frontend/dist)
                           ├─ /api/, /rest/  → API
                           ├─ /media/covers/ custom covers and photos, public
                           └─ /media/        audio by signed link (secure_link, Range)

API ──────────── PostgreSQL ──────────── worker
 ├─ Deezer catalogue + admin edits        ├─ yt-dlp: find and download
 ├─ streams tracks not downloaded yet     └─ ffmpeg: remux, measure loudness
 ├─ Deezer images via /api/img
 ├─ lyrics, Last.fm
 └─ Subsonic API for apps
```

Two Go processes share one database: the **API** answers the site and the
apps, the **worker** downloads audio. nginx serves the web app and the
downloaded files.

## Catalogue

Everything you browse (search, artists, albums, covers) comes from Deezer's
public API, no key needed. Pages are built from Deezer's answers with the
admin's edits on top (`internal/api/overlay.go`). Everything is keyed by
Deezer ID, so namesakes never merge. Your own uploads get `k…` IDs and sit
next to the catalogue.

Deezer allows 50 requests per 5 s per IP, shared by the API and the worker;
`internal/deezer` keeps to its share. Subsonic apps and imports get their
own part of the limit so they can't starve the site. Tracks, albums,
tracklists and artist pages are cached in the `deezer_cache` table, so the
cache survives restarts. `DEEZER_PROXY` adds a second IP and doubles the
limit.

## Downloads

Jobs live in `ingest_jobs` (`pending → processing → done/failed`), one per
Deezer track; the worker runs three at a time. Likes, saved albums and
playlists queue their tracks. A track played before it's downloaded is
fetched by the API while it streams to you, and the finished file goes to
the worker, so nothing is downloaded twice. Failed jobs are retried about
once a day, three times at most; the rest show up on the admin's "Not
found" page.

**Disk.** Audio nobody played for `AUDIO_KEEP_DAYS` is deleted and
downloaded again from the same video on the next play. Uploads and files an
admin replaced stay. Deezer images aren't stored at all. Tracks with the
same ISRC share one file.

**Audio.** YouTube's format 140 (AAC, about 128 kbit/s) plays everywhere,
iOS Safari included, so it's only remuxed into `.m4a` (`+faststart`),
never re-encoded. If it's missing, the audio is encoded to AAC 192k.
Loudness is measured once and stored; the player uses it when volume
normalisation is on (−14 LUFS).

## Picking the recording

`internal/ytdlp` searches YouTube for "artist title", then with featured
artists, with "topic", with "audio", then "artist album", up to 15
candidates per query. A candidate must:

- be within 4 s of Deezer's duration;
- share more than half of the title's words, ignoring "(feat. …)",
  "- Remastered" and the like;
- on a `<Artist> - Topic` channel, have most of its words in the track
  title (otherwise it's another song by the same artist);
- not be live, a cover, karaoke, sped up and so on, unless the track is.

Topic channels win, then the artist's own channel, then word overlap. If
nothing fits, the job fails: wrong audio is worse than none.

A "warm" Python helper keeps yt-dlp loaded in the API, so a track that
isn't downloaded yet starts in a second or two instead of waiting for
yt-dlp to start.

## Lyrics

`internal/lyrics` asks Deezer, LRCLIB and NetEase at once, and lrc.red
(by ISRC) when `LRCRED_CONTACT` is set. Word-by-word timing beats line
timing, line timing beats plain text; lrc.red's word timing beats Deezer's.
Results are stored per recording. "Not found" is rechecked after a week,
lyrics without word timing (or with it, but not from lrc.red) after a month.

## Apps

`/rest/` speaks the Subsonic API with OpenSubsonic extensions
(`internal/api/subsonic*.go`), reusing the site's handlers, so apps see the
same library. Apps sign in with an app password from Settings → Apps. They
send md5(password + salt), so the password is stored encrypted with a key
derived from `JWT_SECRET`, not hashed. Details: [subsonic.md](subsonic.md).

## Audio links

Downloaded files are served by nginx, not the API. The API hands out links
signed with `MEDIA_SECRET` that expire after `MEDIA_TTL_SECONDS`; nginx's
`secure_link` checks them, so a link can't be guessed or reused for long.
The links are relative, so a track plays at whatever address the site is
opened at.
