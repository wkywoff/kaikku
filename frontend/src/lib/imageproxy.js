const DEEZER_IMAGES = /^https:\/\/(?:cdn-images|e-cdns-images)\.dzcdn\.net\/images\//;

export function proxyImages(value) {
  if (typeof value === 'string') return DEEZER_IMAGES.test(value) ? value.replace(DEEZER_IMAGES, '/api/img/') : value;
  if (Array.isArray(value)) return value.map(proxyImages);
  if (value && typeof value === 'object') {
    const out = {};
    for (const [k, v] of Object.entries(value)) out[k] = proxyImages(v);
    return out;
  }
  return value;
}

const SIZED = /^(\/api\/img\/[a-z]+\/[0-9a-f]{32}\/)\d+x\d+-/;
const LOCAL_COVER = /^\/media\/(covers\/.+\.jpg)$/;

export function sized(url, px) {
  if (!url) return url;
  if (LOCAL_COVER.test(url)) return url.replace(LOCAL_COVER, `/api/cover/${px}/$1`);
  return url.replace(SIZED, `$1${px}x${px}-`);
}

export function artworkFor(url) {
  if (!url) return [];
  return [200, 300, 640].map((px) => ({ src: sized(url, px), sizes: `${px}x${px}`, type: 'image/jpeg' }));
}

export function srcsetOf(url, widths) {
  if (!url || !(LOCAL_COVER.test(url) || SIZED.test(url))) return undefined;
  return widths.map((px) => `${sized(url, px)} ${px}w`).join(', ');
}

export const CARD_SIZES = '(max-width: 768px) calc(50vw - 32px), 180px';
