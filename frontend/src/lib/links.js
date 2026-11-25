const ID = '(\\d+|k[1-9]\\d*)';
const bare = new RegExp(`^${ID}$`);

export const isLocalId = (id) => /^k[1-9]\d*$/.test(String(id ?? ''));

function idFrom(text, kind) {
  const s = text.trim();
  if (bare.test(s)) return s;
  return s.match(new RegExp(`/${kind}/${ID}(?:[/?#]|$)`))?.[1] || null;
}

export function trackIdFrom(text) {
  const s = text.trim();
  if (bare.test(s)) return s;
  return s.match(new RegExp(`[?&]track=${ID}(?:&|#|$)`))?.[1] || idFrom(s, 'track');
}

export const albumIdFrom = (text) => idFrom(text, 'album');

export const artistIdFrom = (text) => idFrom(text, 'artist');

export const isShortDeezerLink = (text) => /link\.deezer\.com|deezer\.page\.link/.test(text);
