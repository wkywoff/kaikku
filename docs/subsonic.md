# Subsonic apps

kaikku speaks the Subsonic API (version 1.16.1) with OpenSubsonic
extensions, so you can listen in apps such as Feishin (desktop), Amperfy
(iPhone), Symfonium (Android) and others.

## Connecting

1. On the site: **Settings → Apps → Turn on**. It shows the server
   address, your username and a separate password for apps only (your site
   password doesn't work there; **New password** replaces it).
2. In the app, add a Subsonic (or Navidrome/OpenSubsonic) server with
   those three values.

Apps may send the password as a token with salt (the default) or as plain
text (`p=` or `p=enc:`). API keys (`apiKey`) aren't supported. Failed
logins are counted per username, as on the site.

## What apps see

- **Library**: your liked songs, saved albums and followed artists.
- **Search and artist pages**: the whole Deezer catalogue, plus uploaded
  releases.
- **Playlists**: your own, those you collaborate on and your friends'
  public playlists.
- **Stars**: starring a song likes it on the site, and the other way round.
- **Plays**: plays from apps count in your history and mixes (not on
  Last.fm: only the site scrobbles there).
- **Lyrics**: synced where available (`songLyrics`).
- **Streaming**: always the same AAC `.m4a`; tracks that aren't downloaded
  yet stream while the server downloads them. `format` and `maxBitRate` are
  ignored, and `download` returns the same file.

## Supported methods

| Group | Methods |
|---|---|
| System | `ping`, `getLicense`, `getOpenSubsonicExtensions`, `getMusicFolders`, `getUser` (yourself only), `getScanStatus`, `startScan` |
| Browsing | `getArtists`, `getIndexes`, `getArtist`, `getAlbum`, `getSong`, `getMusicDirectory`, `getAlbumList`, `getAlbumList2`, `getGenres`, `getSongsByGenre` |
| Stars and search | `getStarred`, `getStarred2`, `star`, `unstar`, `search2`, `search3` |
| Playlists | `getPlaylists`, `getPlaylist`, `createPlaylist`, `updatePlaylist`, `deletePlaylist` |
| Playback | `stream`, `download`, `getCoverArt`, `scrobble` |
| Lyrics | `getLyrics`, `getLyricsBySongId` |
| Discovery | `getSimilarSongs`, `getSimilarSongs2`, `getTopSongs`, `getRandomSongs`, `getArtistInfo`, `getArtistInfo2`, `getAlbumInfo`, `getAlbumInfo2` |

Album lists (`getAlbumList`, `getAlbumList2`): `newest`, `recent`,
`frequent`, `highest` (same as `frequent`: there are no ratings), `random`,
`starred`, `alphabeticalByName`, `alphabeticalByArtist`, `byYear`,
`byGenre`.

Answered but always empty: `getNowPlaying`, `getBookmarks`, `getShares`,
`getPlayQueue` / `savePlayQueue`, `getInternetRadioStations`,
`getPodcasts`, `getNewestPodcasts`. Anything else answers "not
implemented".

OpenSubsonic extensions: `formPost`, `songLyrics`. Responses are XML, or
JSON with `f=json`.

## Artwork

Covers come through the site (`getCoverArt`). For full image links in
artist info, set `MEDIA_BASE_URL` to the site's public address
([configuration.md](configuration.md)).
