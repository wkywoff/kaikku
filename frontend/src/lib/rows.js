import { paths } from './route.js';

// Строка списка треков (см. TrackList) из трека Deezer, как его отдаёт API:
// все исполнители (contributors, если есть) — отдельными ссылками.

export function artistsOf(t) {
  if (t.contributors?.length) {
    return t.contributors.map((c) => ({ id: c.id || null, name: c.name, href: c.id ? paths.artist(c.id) : null }));
  }
  return [{ id: t.artist_id || null, name: t.artist, href: t.artist_id ? paths.artist(t.artist_id) : null }];
}

/** Строка для своих списков (любимые, плейлист): номер — позиция в списке, а не в альбоме, без «Диск N». */
export function rowFromCollectionTrack(t) {
  return {
    ...rowFromDeezerTrack(t),
    trackNo: null,
    discNo: null,
    itemId: t.item_id || null,
    addedAt: t.added_at || null,
    addedBy: t.added_by?.username || null, // совместный плейлист — кто добавил
  };
}

/** album — {id, title, cover_url}, если трек из треклиста альбома (там нет своих полей альбома). */
export function rowFromDeezerTrack(t, album = null) {
  const albumId = album?.id || t.album_id;
  return {
    deezerId: t.id,
    title: t.title,
    artists: artistsOf(t),
    durationMs: t.duration_ms,
    trackNo: t.track_no || null, // в топе артиста и поиске Deezer номера не отдаёт (0)
    discNo: t.disc_no || null,
    explicit: t.explicit,
    coverUrl: album?.cover_url || t.cover_url,
    album: { title: album?.title || t.album, href: albumId ? paths.album(albumId) : null },
    hidden: !!t.hidden, // релиз скрыт админом: вне режима админа — серый, не играет
  };
}

// Поиск внутри своего списка (плейлист, любимые): по названию, исполнителям
// и альбому, без учёта регистра, ё/е и диакритики («beyonce» найдёт «Beyoncé»).
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
