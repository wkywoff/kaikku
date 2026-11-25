import { get } from 'svelte/store';
import { auth } from './stores/auth.js';
import { canEdit } from './stores/me.js';
import { proxyImages } from './imageproxy.js';

const BASE = '/api';

function renew(token, fresh) {
  if (fresh && token) auth.renew(token, fresh);
}

async function request(path, options = {}) {
  const token = get(auth);
  const headers = { ...(options.headers || {}) };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  if (get(canEdit)) headers['X-Admin-Mode'] = '1';

  let body = options.body;
  if (body instanceof Blob) {
    headers['Content-Type'] = body.type || 'application/octet-stream';
  } else if (body && typeof body === 'object') {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(body);
  }

  const res = await fetch(BASE + path, { ...options, headers, body });
  renew(token, res.headers.get('X-Auth-Token'));

  if (res.status === 401 && token && get(auth) === token) {
    auth.logout();
  }

  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    let data = null;
    try {
      data = await res.json();
      if (data.error) message = data.error;
    } catch {
    }
    const err = new Error(message);
    err.status = res.status;
    err.data = data;
    err.retryAfter = Number(res.headers?.get?.('Retry-After')) || 0;
    throw err;
  }

  if (res.status === 204) return null;
  return proxyImages(await res.json());
}

function sendFile(method, path, file, { onProgress, signal } = {}) {
  return new Promise((resolve, reject) => {
    const token = get(auth);
    const xhr = new XMLHttpRequest();
    xhr.open(method, BASE + path);
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);
    if (get(canEdit)) xhr.setRequestHeader('X-Admin-Mode', '1');
    xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream');
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total);
    xhr.upload.onload = () => onProgress?.(1);
    xhr.onload = () => {
      let data = null;
      try {
        data = JSON.parse(xhr.responseText);
      } catch {
      }
      renew(token, xhr.getResponseHeader('X-Auth-Token'));
      if (xhr.status >= 200 && xhr.status < 300) return resolve(proxyImages(data));
      if (xhr.status === 401 && token && get(auth) === token) auth.logout();
      const err = new Error(data?.error || `HTTP ${xhr.status}`);
      err.status = xhr.status;
      err.data = data;
      err.retryAfter = Number(xhr.getResponseHeader('Retry-After')) || 0;
      reject(err);
    };
    xhr.onerror = () => reject(new Error('Network error'));
    xhr.onabort = () => reject(new DOMException('aborted', 'AbortError'));
    signal?.addEventListener('abort', () => xhr.abort(), { once: true });
    xhr.send(file);
  });
}

const enc = encodeURIComponent;

export const api = {
  health: () => request('/health'),
  registerInfo: () => request('/register'),
  register: (username, password, invite) => request('/register', { method: 'POST', body: { username, password, invite } }),
  me: () => request('/me'),
  createInvite: () => request('/invites', { method: 'POST' }),
  login: (username, password) => request('/login', { method: 'POST', body: { username, password } }),
  changePassword: (oldPassword, newPassword) =>
    request('/me/password', { method: 'PUT', body: { old_password: oldPassword, new_password: newPassword } }),
  resetPassword: (code, password) => request('/password-reset', { method: 'POST', body: { code, password } }),
  adminUsers: () => request('/admin/users'),
  createPasswordReset: (userId) => request(`/admin/users/${enc(userId)}/password-reset`, { method: 'POST' }),
  setUserAdmin: (userId, admin) => request(`/admin/users/${enc(userId)}/admin`, { method: 'PUT', body: { admin } }),
  lyrics: (deezerId) => request(`/lyrics/${enc(deezerId)}`),

  search: (q, { signal } = {}) => request(`/search?q=${enc(q)}`, { signal }),
  ingestLookup: (deezerTrackIds) => request(`/ingest/lookup?ids=${deezerTrackIds.map(enc).join(',')}`),

  collection: () => request('/me/collection'),
  likedTracks: () => request('/me/liked'),
  like: (deezerTrackId) => request(`/me/liked/${enc(deezerTrackId)}`, { method: 'PUT' }),
  unlike: (deezerTrackId) => request(`/me/liked/${enc(deezerTrackId)}`, { method: 'DELETE' }),
  savedAlbums: () => request('/me/albums'),
  saveAlbum: (deezerAlbumId) => request(`/me/albums/${enc(deezerAlbumId)}`, { method: 'PUT' }),
  unsaveAlbum: (deezerAlbumId) => request(`/me/albums/${enc(deezerAlbumId)}`, { method: 'DELETE' }),
  followedArtists: () => request('/me/artists'),
  artistLiked: (deezerArtistId) => request(`/me/artists/${enc(deezerArtistId)}/liked`),
  follow: (deezerArtistId) => request(`/me/artists/${enc(deezerArtistId)}`, { method: 'PUT' }),
  unfollow: (deezerArtistId) => request(`/me/artists/${enc(deezerArtistId)}`, { method: 'DELETE' }),
  newReleases: () => request('/me/releases'),
  recordPlay: (deezerTrackId, context) => request('/me/plays', { method: 'POST', body: { deezer_id: deezerTrackId, context: context || {} } }),
  playListened: (playId, ms) => request(`/me/plays/${enc(playId)}/listened`, { method: 'POST', body: { ms }, keepalive: true }),
  recent: () => request('/me/recent'),
  libraryTimes: () => request('/me/library/times'),
  mixes: () => request('/me/mixes'),
  genres: () => request('/genres'),
  genre: (slug) => request(`/genres/${enc(slug)}`),
  mix: (id) => request(`/me/mixes/${enc(id)}`),
  lastfm: (cb) => request(`/me/lastfm?cb=${enc(cb)}`),
  lastfmLink: (token) => request('/me/lastfm', { method: 'POST', body: { token } }),
  lastfmSettings: (patch) => request('/me/lastfm', { method: 'PATCH', body: patch }),
  lastfmUnlink: () => request('/me/lastfm', { method: 'DELETE' }),
  subsonic: () => request('/me/subsonic'),
  newSubsonicPassword: () => request('/me/subsonic', { method: 'POST' }),
  disableSubsonic: () => request('/me/subsonic', { method: 'DELETE' }),
  lastfmNowPlaying: (deezerTrackId) => request('/me/now-playing', { method: 'POST', body: { deezer_id: deezerTrackId } }),
  lastfmScrobble: (deezerTrackId, startedAt) => request('/me/scrobbles', { method: 'POST', body: { deezer_id: deezerTrackId, started_at: startedAt } }),
  wrapped: (year) => {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
    return request(`/me/wrapped?tz=${enc(tz)}${year ? `&year=${enc(year)}` : ''}`);
  },
  wrappedAlbums: (year) => {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
    return request(`/me/wrapped/albums?tz=${enc(tz)}&year=${enc(year)}`);
  },
  suggestions: () => request('/me/suggestions'),
  playlists: () => request('/playlists'),
  createPlaylist: (fields) => request('/playlists', { method: 'POST', body: fields }),
  getPlaylist: (id) => request(`/playlists/${enc(id)}`),
  updatePlaylist: (id, fields) => request(`/playlists/${enc(id)}`, { method: 'PUT', body: fields }),
  deletePlaylist: (id) => request(`/playlists/${enc(id)}`, { method: 'DELETE' }),
  addToPlaylist: (id, deezerIds, { download } = {}) =>
    request(`/playlists/${enc(id)}/tracks`, { method: 'POST', body: { deezer_ids: deezerIds, ...(download === false ? { download } : {}) } }),
  importSpotify: (link) => request(`/import/spotify?link=${enc(link)}`),
  importMatch: (tracks) => request('/import/match', { method: 'POST', body: { tracks } }),
  importAlbums: (tracks) => request('/import/albums', { method: 'POST', body: { tracks } }),
  removeFromPlaylist: (id, itemId) => request(`/playlists/${enc(id)}/tracks/${enc(itemId)}`, { method: 'DELETE' }),
  movePlaylistItem: (id, itemId, beforeItemId) =>
    request(`/playlists/${enc(id)}/tracks/${enc(itemId)}/position`, { method: 'PUT', body: { before: beforeItemId } }),
  uploadPlaylistCover: (id, blob) => request(`/playlists/${enc(id)}/cover`, { method: 'PUT', body: blob }),
  deletePlaylistCover: (id) => request(`/playlists/${enc(id)}/cover`, { method: 'DELETE' }),
  profile: (username) => request(`/users/${enc(username)}`),
  followUser: (id) => request(`/users/${enc(id)}/follow`, { method: 'PUT' }),
  unfollowUser: (id) => request(`/users/${enc(id)}/follow`, { method: 'DELETE' }),
  people: () => request('/me/people'),
  addCollaborator: (id, userId) => request(`/playlists/${enc(id)}/collaborators/${enc(userId)}`, { method: 'PUT' }),
  removeCollaborator: (id, userId) => request(`/playlists/${enc(id)}/collaborators/${enc(userId)}`, { method: 'DELETE' }),

  discoverArtist: (id) => request(`/discover/artists/${enc(id)}`),
  discoverAlbum: (id) => request(`/discover/albums/${enc(id)}`),
  discoverArtistAlbums: (id) => request(`/discover/artists/${enc(id)}/albums`),
  discoverTrack: (id) => request(`/discover/tracks/${enc(id)}`),
  relatedArtists: (id) => request(`/discover/artists/${enc(id)}/related`),
  artistBio: (id, lang) => request(`/discover/artists/${enc(id)}/bio?lang=${enc(lang)}`),
  setArtistBio: (id, text) => request(`/discover/artists/${enc(id)}/bio`, { method: 'PUT', body: { text } }),
  resetArtistBio: (id) => request(`/discover/artists/${enc(id)}/bio`, { method: 'DELETE' }),
  setArtistTags: (id, tags) => request(`/discover/artists/${enc(id)}/tags`, { method: 'PUT', body: { tags } }),
  resetArtistTags: (id) => request(`/discover/artists/${enc(id)}/tags`, { method: 'DELETE' }),
  artistRadio: (id) => request(`/discover/artists/${enc(id)}/radio`),

  getArtist: (id) => request(`/artists/${enc(id)}`),
  getAlbum: (id) => request(`/albums/${enc(id)}`),

  streamUrl: (trackId) => request(`/tracks/${enc(trackId)}/stream-url`),
  deezerStream: (deezerTrackId) => request(`/stream/deezer/${enc(deezerTrackId)}/url`),

  updateArtist: (id, fields) => request(`/artists/${enc(id)}`, { method: 'PUT', body: fields }),
  updateAlbum: (id, fields) => request(`/albums/${enc(id)}`, { method: 'PUT', body: fields }),
  resetArtist: (id) => request(`/artists/${enc(id)}/reset`, { method: 'POST' }),
  claim: (kind, deezerId) => request(`/${kind}s/deezer/${enc(deezerId)}/claim`, { method: 'POST' }),
  getDeezerTrackEdit: (deezerId) => request(`/tracks/deezer/${enc(deezerId)}/edit`),
  updateDeezerTrack: (deezerId, fields) => request(`/tracks/deezer/${enc(deezerId)}`, { method: 'PUT', body: fields }),
  resetDeezerTrack: (deezerId) => request(`/tracks/deezer/${enc(deezerId)}/reset`, { method: 'POST' }),
  resetAlbum: (id) => request(`/albums/${enc(id)}/reset`, { method: 'POST' }),
  artistInfo: (deezerId) => request(`/discover/artists/${enc(deezerId)}/info`),
  setAlbumHidden: (deezerId, hidden) => request(`/albums/deezer/${enc(deezerId)}/hidden`, { method: 'PUT', body: { hidden } }),
  refreshAlbum: (deezerId) => request(`/albums/deezer/${enc(deezerId)}/refresh`, { method: 'POST' }),
  redownloadTrack: (deezerId) => request(`/tracks/deezer/${enc(deezerId)}/redownload`, { method: 'POST' }),
  replaceAlbumTrack: (albumDeezerId, track, withId) =>
    request(`/albums/deezer/${enc(albumDeezerId)}/tracklist/replace`, { method: 'POST', body: { track, with: withId } }),
  missing: () => request('/admin/missing'),
  retryMissing: (jobId) => request(`/admin/missing/${enc(jobId)}/retry`, { method: 'POST' }),
  uploadYouTubeCookies: (file) => request('/admin/youtube/cookies', { method: 'PUT', body: file }),
  deleteYouTubeCookies: () => request('/admin/youtube/cookies', { method: 'DELETE' }),
  setTrackVideo: (deezerId, url) => request(`/tracks/deezer/${enc(deezerId)}/video`, { method: 'PUT', body: { url } }),
  uploadTrackAudio: (deezerId, file, onProgress) => sendFile('PUT', `/tracks/deezer/${enc(deezerId)}/audio`, file, { onProgress }),
  refreshLyrics: (deezerId) => request(`/lyrics/${enc(deezerId)}/refresh`, { method: 'POST' }),
  uploadArtistPicture: (id, blob) => request(`/artists/${enc(id)}/picture`, { method: 'PUT', body: blob }),
  deleteAlbumCover: (id) => request(`/albums/${enc(id)}/cover`, { method: 'DELETE' }),
  uploadAlbumCover: (id, blob) => request(`/albums/${enc(id)}/cover`, { method: 'PUT', body: blob }),

  uploads: () => request('/uploads'),
  createRelease: () => request('/uploads/releases', { method: 'POST' }),
  getRelease: (id) => request(`/uploads/releases/${enc(id)}`),
  saveRelease: (id, body) => request(`/uploads/releases/${enc(id)}`, { method: 'PUT', body }),
  deleteRelease: (id) => request(`/uploads/releases/${enc(id)}`, { method: 'DELETE' }),
  deleteReleaseCover: (id) => request(`/uploads/releases/${enc(id)}/cover`, { method: 'DELETE' }),
  uploadReleaseCover: (id, blob) => request(`/uploads/releases/${enc(id)}/cover`, { method: 'PUT', body: blob }),
  addReleaseAudio: (id, file, opts) => sendFile('POST', `/uploads/releases/${enc(id)}/audio?name=${enc(file.name)}`, file, opts),
  replaceTrackAudio: (trackId, file, opts) => sendFile('PUT', `/uploads/tracks/${enc(trackId)}/audio`, file, opts),
  getOwnTrack: (trackId) => request(`/uploads/tracks/${enc(trackId)}`),
  saveOwnTrack: (trackId, fields) => request(`/uploads/tracks/${enc(trackId)}`, { method: 'PUT', body: fields }),
  deleteUploadTrack: (trackId) => request(`/uploads/tracks/${enc(trackId)}`, { method: 'DELETE' }),
  trackLyricsText: (trackId) => request(`/uploads/tracks/${enc(trackId)}/lyrics`),
  saveTrackLyrics: (trackId, text) => request(`/uploads/tracks/${enc(trackId)}/lyrics`, { method: 'PUT', body: { text } }),
  createArtist: (name) => request('/uploads/artists', { method: 'POST', body: { name } }),
  searchArtists: (q, { signal } = {}) => request(`/search/artists?q=${enc(q)}`, { signal }),
};
