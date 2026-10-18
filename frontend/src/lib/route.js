// Маршруты приложения. Адрес в строке браузера — источник истины для того,
// какой экран открыт, поэтому «назад»/«вперёд» браузера, кнопки мыши и
// жесты работают как на обычном сайте, а на любую страницу можно дать ссылку.
//
//   /                          → главная
//   /library                   → медиатека: любимые, плейлисты, альбомы, исполнители
//   /search?q=…                → поиск: треки, артисты, альбомы (Deezer)
//   /settings                  → настройки
//   /lastfm?token=…            → настройки: Last.fm вернул после «Разрешить доступ»
//   /collection/tracks         → любимые треки
//   /playlist/<uuid>           → плейлист
//   /mix/<id>                  → микс: on-repeat, forgotten, tag-<жанр> (internal/api/mixes.go)
//   /genre/<slug>              → жанр (теги Last.fm, internal/api/genres.go)
//   /artist/<deezer id>        → страница артиста (?disc=album|ep|single — сразу с фильтром дискографии)
//   /artist/<id>/discography/<all|album|ep|single> → вся дискография (как у Spotify)
//   /album/<deezer id>         → страница альбома (?track=<deezer id> — с выделенным треком: ссылка «Поделиться»)
//   /upload, /upload/<id>      → свои релизы: список и окно загрузки (id — k…)
//   /admin/missing             → «Не нашлось» (админ): треки, которые не скачались
//
// ID артистов, альбомов и треков — Deezer (число) или свои, загруженные (k…).
//   /wrapped, /wrapped/<год>   → итоги года
//   /user/<ник>                → профиль: подписка, публичные плейлисты
//
// Старые адреса /discover/… продолжают открываться.
//
// route: { view: 'home'|'library'|'search'|'settings'|'liked'|'playlist'|'mix'|'genre'|'artist'|'discography'|'album'|'wrapped'|'uploads'|'upload'|'missing'|'user', id?, q?, filter?, track?, year? }

export function parseRoute(pathname, search = '') {
  const parts = pathname.split('/').filter(Boolean).map(decodeURIComponent);
  const q = new URLSearchParams(search).get('q') || '';
  const [a, b, c, d] = parts;

  if (a === 'search') return { view: 'search', q };
  if (a === 'library' && !b) return { view: 'library' };
  if (a === 'settings') return { view: 'settings' };
  if (a === 'lastfm') return { view: 'settings', lastfm: true }; // возврат с Last.fm (?token=…), см. SettingsPage
  if (a === 'admin' && b === 'missing') return { view: 'missing' };
  if (a === 'upload') return b ? { view: 'upload', id: b } : { view: 'uploads' };
  if (a === 'wrapped') return /^\d{4}$/.test(b || '') ? { view: 'wrapped', year: b } : { view: 'wrapped' };
  if (a === 'collection' && b === 'tracks') return { view: 'liked' };
  if (a === 'playlist' && b) return { view: 'playlist', id: b };
  if (a === 'mix' && b) return { view: 'mix', id: b };
  if (a === 'genre' && b) return { view: 'genre', id: b };
  if (a === 'user' && b) return { view: 'user', id: b };
  if (a === 'artist' && b && c === 'discography') {
    return { view: 'discography', id: b, filter: DISCOGRAPHY_FILTERS.includes(d) ? d : 'all' };
  }
  if (a === 'artist' && b) {
    const disc = new URLSearchParams(search).get('disc');
    return ['album', 'ep', 'single'].includes(disc) ? { view: 'artist', id: b, disc } : { view: 'artist', id: b };
  }
  if (a === 'album' && b) {
    const track = new URLSearchParams(search).get('track');
    return /^(\d+|k[1-9]\d*)$/.test(track || '') ? { view: 'album', id: b, track } : { view: 'album', id: b };
  }
  if (a === 'discover') {
    if (b === 'artist' && c) return { view: 'artist', id: c };
    if (b === 'album' && c) return { view: 'album', id: c };
    return { view: 'search', q };
  }
  return { view: 'home' };
}

const enc = encodeURIComponent;

export const DISCOGRAPHY_FILTERS = ['all', 'album', 'ep', 'single'];

export const paths = {
  home: () => '/',
  library: () => '/library',
  search: (q = '') => (q ? `/search?q=${enc(q)}` : '/search'),
  settings: () => '/settings',
  liked: () => '/collection/tracks',
  playlist: (id) => `/playlist/${enc(id)}`,
  mix: (id) => `/mix/${enc(id)}`,
  genre: (slug) => `/genre/${enc(slug)}`,
  artist: (deezerId, disc = '') => `/artist/${enc(deezerId)}${disc ? `?disc=${enc(disc)}` : ''}`,
  discography: (deezerId, filter = 'all') => `/artist/${enc(deezerId)}/discography/${enc(filter)}`,
  album: (deezerId) => `/album/${enc(deezerId)}`,
  // albumHref — уже готовый адрес альбома (row.album.href).
  albumTrack: (albumHref, trackId) => `${albumHref}?track=${enc(trackId)}`,
  uploads: () => '/upload',
  missing: () => '/admin/missing',
  upload: (id) => `/upload/${enc(id)}`,
  wrapped: (year = '') => (year ? `/wrapped/${enc(year)}` : '/wrapped'),
  user: (username) => `/user/${enc(username)}`,
};
