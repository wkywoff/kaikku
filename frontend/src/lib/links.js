// ID в каталоге: число — Deezer, «k» + число — свой релиз kaikku (загрузка).
const ID = '(\\d+|k[1-9]\\d*)';
const bare = new RegExp(`^${ID}$`);

/** Свой (загруженный) артист, релиз или трек, а не Deezer. */
export const isLocalId = (id) => /^k[1-9]\d*$/.test(String(id ?? ''));

function idFrom(text, kind) {
  const s = text.trim();
  if (bare.test(s)) return s;
  return s.match(new RegExp(`/${kind}/${ID}(?:[/?#]|$)`))?.[1] || null;
}

// Ссылка на трек из поля правки: deezer.com/…/track/123, ссылка kaikku
// /album/<альбом>?track=123 («Поделиться» у трека) или просто ID.
export function trackIdFrom(text) {
  const s = text.trim();
  if (bare.test(s)) return s;
  return s.match(new RegExp(`[?&]track=${ID}(?:&|#|$)`))?.[1] || idFrom(s, 'track');
}

/** Ссылка на альбом: deezer.com/…/album/123, ссылка kaikku /album/<id> или ID. */
export const albumIdFrom = (text) => idFrom(text, 'album');

/** Ссылка на артиста: deezer.com/…/artist/123, ссылка kaikku /artist/<id> или ID. */
export const artistIdFrom = (text) => idFrom(text, 'artist');

/** Короткие ссылки Deezer (link.deezer.com) не раскрываются без браузера. */
export const isShortDeezerLink = (text) => /link\.deezer\.com|deezer\.page\.link/.test(text);
