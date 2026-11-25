import { paths } from './route.js';

export function artistsOf(t) {
  if (t.contributors?.length) {
    return t.contributors.map((c) => ({ id: c.id || null, name: c.name, href: c.id ? paths.artist(c.id) : null, picture: c.picture_url || null }));
  }
  return [{ id: t.artist_id || null, name: t.artist, href: t.artist_id ? paths.artist(t.artist_id) : null }];
}

export function rowFromCollectionTrack(t) {
  return {
    ...rowFromDeezerTrack(t),
    trackNo: null,
    discNo: null,
    itemId: t.item_id || null,
    addedAt: t.added_at || null,
    addedBy: t.added_by?.username || null,
  };
}

export function rowFromDeezerTrack(t, album = null) {
  const albumId = album?.id || t.album_id;
  return {
    deezerId: t.id,
    title: t.title,
    artists: artistsOf(t),
    durationMs: t.duration_ms,
    trackNo: t.track_no || null,
    discNo: t.disc_no || null,
    explicit: t.explicit,
    coverUrl: album?.cover_url || t.cover_url,
    album: { title: album?.title || t.album, href: albumId ? paths.album(albumId) : null },
    hidden: !!t.hidden,
  };
}

export function normalizeText(s) {
  return String(s || '')
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/ё/g, 'е')
    .trim();
}

export function filterRows(rows, query) {
  const words = normalizeText(query).split(/\s+/).filter(Boolean);
  if (!words.length) return rows;
  return rows.filter((r) => {
    const text = normalizeText([r.title, ...(r.artists || []).map((a) => a.name), r.album?.title].join(' '));
    return words.every((w) => text.includes(w));
  });
}
