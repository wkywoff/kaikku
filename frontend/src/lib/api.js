import { get } from 'svelte/store';
import { auth } from './stores/auth.js';
import { canEdit } from './stores/me.js';
import { proxyImages } from './imageproxy.js';

const BASE = '/api';

// Сервер продлевает вход: токену больше суток — присылает новый
// (X-Auth-Token). Сохраняем, если за время запроса не вошли заново и не вышли.
function renew(token, fresh) {
  if (fresh && token && get(auth) === token) auth.login(fresh);
}

async function request(path, options = {}) {
  const token = get(auth);
  const headers = { ...(options.headers || {}) };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  // В режиме админа сервер отдаёт и скрытые релизы (с пометкой hidden).
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
    // Токен истёк/невалиден — не пытаемся угадать, просто разлогиниваем,
    // чтобы человек увидел форму логина, а не тихую ошибку в списке. Если
    // токен за время запроса сменился (только что сменили пароль), старый
    // ответ не в счёт.
    auth.logout();
  }

  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    let data = null;
    try {
      data = await res.json();
      if (data.error) message = data.error;
    } catch {
      // тело не JSON — оставляем дефолтное сообщение
    }
    const err = new Error(message);
    err.status = res.status; // страница входа показывает по коду понятный текст
    err.data = data; // подробности ответа (окно загрузки: чего не хватает — missing)
    err.retryAfter = Number(res.headers?.get?.('Retry-After')) || 0; // секунд до следующей попытки входа
    throw err;
  }

  if (res.status === 204) return null;
  return proxyImages(await res.json());
}

// sendFile отправляет файл сырым телом через XHR: у fetch нет прогресса
// отправки, а аудио бывает в сотни МБ. onProgress(доля 0..1); 1 — файл
// дошёл, сервер его обрабатывает. signal — отменить.
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
        // nginx отвечает на слишком большой файл HTML-страницей
      }
      if (xhr.status >= 200 && xhr.status < 300) return resolve(proxyImages(data));
      renew(token, xhr.getResponseHeader('X-Auth-Token'));
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
  // Пароль: сменить свой -> { token }; по ссылке сброса -> { token, username }.
  changePassword: (oldPassword, newPassword) =>
    request('/me/password', { method: 'PUT', body: { old_password: oldPassword, new_password: newPassword } }),
  resetPassword: (code, password) => request('/password-reset', { method: 'POST', body: { code, password } }),
  // Админ: кому сбросить пароль, одноразовая ссылка -> { code, username, expires_at }.
  adminUsers: () => request('/admin/users'),
  createPasswordReset: (userId) => request(`/admin/users/${encodeURIComponent(userId)}/password-reset`, { method: 'POST' }),
  // Тексты песен -> { lyrics: { source, sync, lines, writers } | null }.
  lyrics: (deezerId) => request(`/lyrics/${encodeURIComponent(deezerId)}`),

  // signal — отменить, если человек печатает дальше (SearchPage).
  search: (q, { signal } = {}) => request(`/search?q=${encodeURIComponent(q)}`, { signal }), // -> { tracks, artists, albums }
  ingestLookup: (deezerTrackIds) => request(`/ingest/lookup?ids=${deezerTrackIds.join(',')}`),

  // Личная медиатека: лайки, сохранённые альбомы, плейлисты (по Deezer ID;
  // сервер сам берёт метаданные и качает добавленное в фоне).
  collection: () => request('/me/collection'), // -> { liked: [id], albums: [id], artists: [id] }
  likedTracks: () => request('/me/liked'), // -> { tracks }
  like: (deezerTrackId) => request(`/me/liked/${deezerTrackId}`, { method: 'PUT' }),
  unlike: (deezerTrackId) => request(`/me/liked/${deezerTrackId}`, { method: 'DELETE' }),
  savedAlbums: () => request('/me/albums'), // -> { albums }
  saveAlbum: (deezerAlbumId) => request(`/me/albums/${deezerAlbumId}`, { method: 'PUT' }),
  unsaveAlbum: (deezerAlbumId) => request(`/me/albums/${deezerAlbumId}`, { method: 'DELETE' }),
  followedArtists: () => request('/me/artists'), // -> { artists }
  follow: (deezerArtistId) => request(`/me/artists/${deezerArtistId}`, { method: 'PUT' }),
  unfollow: (deezerArtistId) => request(`/me/artists/${deezerArtistId}`, { method: 'DELETE' }),
  newReleases: () => request('/me/releases'), // -> { albums } — свежее у артистов из подписок
  // История: трек дослушан до порога; context — { kind: album|playlist|artist|liked|mix, id }.
  recordPlay: (deezerTrackId, context) => request('/me/plays', { method: 'POST', body: { deezer_id: deezerTrackId, context: context || {} } }),
  recent: () => request('/me/recent'),
  libraryTimes: () => request('/me/library/times'), // -> { added: {"album:ID": ts}, played: {…} } — сортировка медиатеки
  // Миксы — подборки из истории и лайков: { mixes: [{ id, kind, tag?, artists, covers }] };
  // один микс -> { mix, tracks } (404 — такого микса у человека сейчас нет).
  mixes: () => request('/me/mixes'),
  // Жанры (теги Last.fm): плитки -> { genres: [{ slug, name }] }; страница -> { genre, own, artists, tracks, mix_id? }.
  genres: () => request('/genres'),
  genre: (slug) => request(`/genres/${enc(slug)}`),
  mix: (id) => request(`/me/mixes/${enc(id)}`),
  // Last.fm: cb — куда Last.fm вернёт с ?token=… после «Разрешить».
  lastfm: (cb) => request(`/me/lastfm?cb=${encodeURIComponent(cb)}`), // -> { available, auth_url, username?, scrobble?, love?, pending? }
  lastfmLink: (token) => request('/me/lastfm', { method: 'POST', body: { token } }),
  lastfmSettings: (patch) => request('/me/lastfm', { method: 'PATCH', body: patch }),
  lastfmUnlink: () => request('/me/lastfm', { method: 'DELETE' }),
  // Пароль для приложений (Subsonic API): { enabled, username, password? }.
  subsonic: () => request('/me/subsonic'),
  newSubsonicPassword: () => request('/me/subsonic', { method: 'POST' }),
  disableSubsonic: () => request('/me/subsonic', { method: 'DELETE' }),
  lastfmNowPlaying: (deezerTrackId) => request('/me/now-playing', { method: 'POST', body: { deezer_id: deezerTrackId } }),
  lastfmScrobble: (deezerTrackId, startedAt) => request('/me/scrobbles', { method: 'POST', body: { deezer_id: deezerTrackId, started_at: startedAt } }),
  // Итоги года (без year — текущий) в часовом поясе браузера.
  wrapped: (year) => {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
    return request(`/me/wrapped?tz=${encodeURIComponent(tz)}${year ? `&year=${encodeURIComponent(year)}` : ''}`);
  }, // -> { items: [{ kind, album? | artist? | playlist? }] }
  suggestions: () => request('/me/suggestions'), // -> { artists, based_on }
  playlists: () => request('/playlists'), // -> { mine, others }
  createPlaylist: (fields) => request('/playlists', { method: 'POST', body: fields }),
  getPlaylist: (id) => request(`/playlists/${id}`), // -> { playlist, tracks }
  updatePlaylist: (id, fields) => request(`/playlists/${id}`, { method: 'PUT', body: fields }),
  deletePlaylist: (id) => request(`/playlists/${id}`, { method: 'DELETE' }),
  // download: false — не ставить на скачивание (импорт целого плейлиста).
  addToPlaylist: (id, deezerIds, { download } = {}) =>
    request(`/playlists/${id}/tracks`, { method: 'POST', body: { deezer_ids: deezerIds, ...(download === false ? { download } : {}) } }),
  // Импорт: треки плейлиста Spotify по ссылке → { title, tracks, truncated };
  // поиск пачки треков в каталоге → { matches: [трек Deezer | null] }.
  importSpotify: (link) => request(`/import/spotify?link=${encodeURIComponent(link)}`),
  importMatch: (tracks) => request('/import/match', { method: 'POST', body: { tracks } }),
  removeFromPlaylist: (id, itemId) => request(`/playlists/${id}/tracks/${itemId}`, { method: 'DELETE' }),
  // Переставить трек: встать перед beforeItemId (null — в конец).
  movePlaylistItem: (id, itemId, beforeItemId) =>
    request(`/playlists/${id}/tracks/${itemId}/position`, { method: 'PUT', body: { before: beforeItemId } }),
  // Своя обложка — Blob JPEG (lib/image.js → toSquareJpeg); ответ — плейлист.
  uploadPlaylistCover: (id, blob) => request(`/playlists/${id}/cover`, { method: 'PUT', body: blob }),
  deletePlaylistCover: (id) => request(`/playlists/${id}/cover`, { method: 'DELETE' }),
  // Профили и подписки: профиль по нику -> { user, me, playlists, people? };
  // подписаться / отписаться -> человек; свои люди -> { people } (друзья —
  // подписаны в обе стороны: их зовут в соавторы).
  profile: (username) => request(`/users/${enc(username)}`),
  followUser: (id) => request(`/users/${enc(id)}/follow`, { method: 'PUT' }),
  unfollowUser: (id) => request(`/users/${enc(id)}/follow`, { method: 'DELETE' }),
  people: () => request('/me/people'),
  // Совместные плейлисты: позвать друга / убрать (или выйти самому).
  addCollaborator: (id, userId) => request(`/playlists/${id}/collaborators/${enc(userId)}`, { method: 'PUT' }),
  removeCollaborator: (id, userId) => request(`/playlists/${id}/collaborators/${enc(userId)}`, { method: 'DELETE' }),

  discoverArtist: (id) => request(`/discover/artists/${id}`), // -> { artist, albums, top }
  discoverAlbum: (id) => request(`/discover/albums/${id}`), // -> { album, tracks }
  discoverArtistAlbums: (id) => request(`/discover/artists/${id}/albums`), // -> { albums }
  discoverTrack: (id) => request(`/discover/tracks/${id}`), // -> трек со всеми исполнителями
  relatedArtists: (id) => request(`/discover/artists/${id}/related`), // -> { artists }
  // Описание артиста: { text, source: custom|deezer|lastfm|none, url, editable, tags: [жанр] }; text "" — нет.
  artistBio: (id, lang) => request(`/discover/artists/${id}/bio?lang=${lang}`),
  setArtistBio: (id, text) => request(`/discover/artists/${id}/bio`, { method: 'PUT', body: { text } }),
  resetArtistBio: (id) => request(`/discover/artists/${id}/bio`, { method: 'DELETE' }),
  // Свои жанры артиста (до 5) и сброс к автоматическим -> { tags, tags_custom }.
  setArtistTags: (id, tags) => request(`/discover/artists/${id}/tags`, { method: 'PUT', body: { tags } }),
  resetArtistTags: (id) => request(`/discover/artists/${id}/tags`, { method: 'DELETE' }),
  artistRadio: (id) => request(`/discover/artists/${id}/radio`), // -> { tracks }

  // Наши строки артиста/альбома (по id из claim) — для правки админом.
  getArtist: (id) => request(`/artists/${id}`),
  getAlbum: (id) => request(`/albums/${id}`),

  streamUrl: (trackId) => request(`/tracks/${trackId}/stream-url`),
  // Трек не из медиатеки: ставит его на добавление и даёт ссылку на поток
  // во время скачивания (или /media, если он уже есть) — см. internal/api/stream.go.
  deezerStream: (deezerTrackId) => request(`/stream/deezer/${deezerTrackId}/url`),

  // Правка (админ). Картинка — Blob JPEG (lib/image.js → toSquareJpeg).
  updateArtist: (id, fields) => request(`/artists/${id}`, { method: 'PUT', body: fields }),
  updateAlbum: (id, fields) => request(`/albums/${id}`, { method: 'PUT', body: fields }),
  resetArtist: (id) => request(`/artists/${id}/reset`, { method: 'POST' }),
  // Артист/альбом, которых ещё нет в медиатеке: завести запись по Deezer ID → { id }.
  claim: (kind, deezerId) => request(`/${kind}s/deezer/${deezerId}/claim`, { method: 'POST' }),
  // Трек правится по Deezer ID, скачан он или нет (сервер сам решает, где хранить правку).
  getDeezerTrackEdit: (deezerId) => request(`/tracks/deezer/${deezerId}/edit`),
  updateDeezerTrack: (deezerId, fields) => request(`/tracks/deezer/${deezerId}`, { method: 'PUT', body: fields }),
  resetDeezerTrack: (deezerId) => request(`/tracks/deezer/${deezerId}/reset`, { method: 'POST' }),
  resetAlbum: (id) => request(`/albums/${id}/reset`, { method: 'POST' }),
  // Артист Deezer по ID (имя, фото) — для правки исполнителей по ссылке.
  artistInfo: (deezerId) => request(`/discover/artists/${encodeURIComponent(deezerId)}/info`),
  // Скрыть/показать релиз (по Deezer ID) -> { id, hidden }.
  setAlbumHidden: (deezerId, hidden) => request(`/albums/deezer/${deezerId}/hidden`, { method: 'PUT', body: { hidden } }),
  // Забыть релиз в кеше Deezer: следующий заход возьмёт его с Deezer заново.
  refreshAlbum: (deezerId) => request(`/albums/deezer/${deezerId}/refresh`, { method: 'POST' }),
  // Перекачать аудио трека из медиатеки (заново найти видео) -> { status: 'queued' }.
  redownloadTrack: (deezerId) => request(`/tracks/deezer/${deezerId}/redownload`, { method: 'POST' }),
  // Трек релиза — другим треком Deezer (не та версия) -> { album, tracks }.
  replaceAlbumTrack: (albumDeezerId, track, withId) =>
    request(`/albums/deezer/${albumDeezerId}/tracklist/replace`, { method: 'POST', body: { track, with: withId } }),
  // «Не нашлось» (админ): упавшие скачивания -> { tracks }; повторить; скачать
  // трек с видео по ссылке YouTube.
  missing: () => request('/admin/missing'),
  retryMissing: (jobId) => request(`/admin/missing/${enc(jobId)}/retry`, { method: 'POST' }),
  setTrackVideo: (deezerId, url) => request(`/tracks/deezer/${enc(deezerId)}/video`, { method: 'PUT', body: { url } }),
  // Заменить аудио трека своим файлом (или дать его треку, который не
  // нашёлся) -> { status: replaced|added, gain_db }.
  // Через XHR: у fetch нет прогресса отправки, а файл бывает в десятки МБ.
  uploadTrackAudio: (deezerId, file, onProgress) => sendFile('PUT', `/tracks/deezer/${deezerId}/audio`, file, { onProgress }),
  // Заново найти текст -> как lyrics().
  refreshLyrics: (deezerId) => request(`/lyrics/${encodeURIComponent(deezerId)}/refresh`, { method: 'POST' }),
  uploadArtistPicture: (id, blob) => request(`/artists/${id}/picture`, { method: 'PUT', body: blob }),
  uploadAlbumCover: (id, blob) => request(`/albums/${id}/cover`, { method: 'PUT', body: blob }),

  // Свои релизы (загрузка): черновик → треки и поля → публикация.
  uploads: () => request('/uploads'), // -> { releases: [album + draft] }
  createRelease: () => request('/uploads/releases', { method: 'POST' }), // -> { id }
  getRelease: (id) => request(`/uploads/releases/${enc(id)}`),
  // body: { title, artists: [id], record_type, release_date, tracks: [{ id, title, artists|null, explicit }], published? }
  saveRelease: (id, body) => request(`/uploads/releases/${enc(id)}`, { method: 'PUT', body }),
  deleteRelease: (id) => request(`/uploads/releases/${enc(id)}`, { method: 'DELETE' }),
  uploadReleaseCover: (id, blob) => request(`/uploads/releases/${enc(id)}/cover`, { method: 'PUT', body: blob }),
  // Новый трек из файла -> { track, tags, cover_url? }.
  addReleaseAudio: (id, file, opts) => sendFile('POST', `/uploads/releases/${enc(id)}/audio?name=${enc(file.name)}`, file, opts),
  replaceTrackAudio: (trackId, file, opts) => sendFile('PUT', `/uploads/tracks/${enc(trackId)}/audio`, file, opts),
  deleteUploadTrack: (trackId) => request(`/uploads/tracks/${enc(trackId)}`, { method: 'DELETE' }),
  trackLyricsText: (trackId) => request(`/uploads/tracks/${enc(trackId)}/lyrics`), // -> { text }
  saveTrackLyrics: (trackId, text) => request(`/uploads/tracks/${enc(trackId)}/lyrics`, { method: 'PUT', body: { text } }),
  createArtist: (name) => request('/uploads/artists', { method: 'POST', body: { name } }),
  // Артисты для выбора исполнителей: свои, потом Deezer.
  searchArtists: (q, { signal } = {}) => request(`/search/artists?q=${enc(q)}`, { signal }),
};
