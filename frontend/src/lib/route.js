export function parseRoute(pathname, search = '') {
  const parts = pathname.split('/').filter(Boolean).map(decodeURIComponent);
  const q = new URLSearchParams(search).get('q') || '';
  const [a, b, c, d] = parts;

  if (a === 'search') return { view: 'search', q };
  if (a === 'library' && !b) return { view: 'library' };
  if (a === 'settings') return { view: 'settings' };
  if (a === 'lastfm') return { view: 'settings', lastfm: true };
  if (a === 'admin' && b === 'missing') return { view: 'missing' };
  if (a === 'upload') return b ? { view: 'releaseEdit', id: b } : { view: 'uploads' };
  if (a === 'wrapped') {
    if (!/^\d{4}$/.test(b || '')) return { view: 'wrapped' };
    return c === 'albums' ? { view: 'wrappedAlbums', year: b } : { view: 'wrapped', year: b };
  }
  if (a === 'collection' && b === 'tracks') return { view: 'liked' };
  if (a === 'playlist' && b) return { view: 'playlist', id: b };
  if (a === 'mix' && b) return { view: 'mix', id: b };
  if (a === 'genre' && b) return { view: 'genre', id: b };
  if (a === 'user' && b) return { view: 'user', id: b };
  if (a === 'artist' && b && c === 'discography') {
    return { view: 'discography', id: b, filter: DISCOGRAPHY_FILTERS.includes(d) ? d : 'all' };
  }
  if (a === 'artist' && b && c === 'liked') return { view: 'artistLiked', id: b };
  if (a === 'artist' && b) {
    const disc = new URLSearchParams(search).get('disc');
    return ['album', 'ep', 'single'].includes(disc) ? { view: 'artist', id: b, disc } : { view: 'artist', id: b };
  }
  if (a === 'album' && b && c === 'edit') return { view: 'releaseEdit', id: b };
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
  artistLiked: (deezerId) => `/artist/${enc(deezerId)}/liked`,
  discography: (deezerId, filter = 'all') => `/artist/${enc(deezerId)}/discography/${enc(filter)}`,
  album: (deezerId) => `/album/${enc(deezerId)}`,
  albumTrack: (albumHref, trackId) => `${albumHref}?track=${enc(trackId)}`,
  uploads: () => '/upload',
  missing: () => '/admin/missing',
  releaseEdit: (deezerId) => `/album/${enc(deezerId)}/edit`,
  wrapped: (year = '') => (year ? `/wrapped/${enc(year)}` : '/wrapped'),
  wrappedAlbums: (year) => `/wrapped/${enc(year)}/albums`,
  user: (username) => `/user/${enc(username)}`,
};
