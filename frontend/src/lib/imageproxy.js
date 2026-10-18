// Картинки каталога Deezer грузятся через наш сервер (/api/img/...), а не с
// CDN Deezer: у части людей он недоступен, а наш домен открывается (см.
// internal/api/images.go). Переписываем ссылки прямо в ответах API — так
// не нужно помнить об этом в каждом компоненте.

const DEEZER_IMAGES = /^https:\/\/(?:cdn-images|e-cdns-images)\.dzcdn\.net\/images\//;

/** Вернуть копию JSON-ответа, где ссылки на картинки Deezer ведут на /api/img/. */
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

/**
 * Та же картинка Deezer нужного размера (CDN отдаёт любой квадрат): не
 * гонять 1000×1000 по 150–350 КБ туда, где она занимает 40–300 px. Размеры —
 * как у Spotify: 64 для мелких, 300 для карточек и шапки альбома, 320 для
 * фото артиста, 640 для полноэкранных. Скачанные обложки (/media/covers/...)
 * уменьшает наш сервер (internal/api/covers.go).
 */
export function sized(url, px) {
  if (!url) return url;
  if (LOCAL_COVER.test(url)) return url.replace(LOCAL_COVER, `/api/cover/${px}/$1`);
  return url.replace(SIZED, `$1${px}x${px}-`);
}

/**
 * Обложка для экрана блокировки, CarPlay и часов (Media Session): несколько
 * размеров — система берёт подходящий, а не уменьшает 640 сама. Только
 * размеры, которые отдаёт и наш сервер (covers.go), и прокси Deezer (images.go).
 */
export function artworkFor(url) {
  if (!url) return [];
  return [200, 300, 640].map((px) => ({ src: sized(url, px), sizes: `${px}x${px}`, type: 'image/jpeg' }));
}

/**
 * srcset из той же картинки нескольких размеров (sized): браузер сам берёт
 * нужную экрану. Карточка на компьютере ~160 px — на обычном мониторе ей
 * хватает 200, на «ретине» и телефоне берётся 300, как раньше. undefined —
 * картинку не уменьшить (своя ссылка вроде blob:), хватит src.
 */
export function srcsetOf(url, widths) {
  if (!url || !(LOCAL_COVER.test(url) || SIZED.test(url))) return undefined;
  return widths.map((px) => `${sized(url, px)} ${px}w`).join(', ');
}

// Ширина картинки карточки (MediaCard, Artwork): сетка minmax(176px, 1fr) —
// картинка ~152–180 px; на телефоне две колонки — полэкрана минус отступы.
export const CARD_SIZES = '(max-width: 768px) calc(50vw - 32px), 180px';
