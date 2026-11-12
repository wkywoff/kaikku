import { translate } from './i18n.js';
import { artistsOf } from './rows.js';
import { sized } from './imageproxy.js';
import { num, word } from './wrappedText.js';

// Картинка «Мои итоги года» для сторис и мессенджеров (1080×1920): топ-5
// треков с обложками, ряд из пяти альбомов и пяти артистов, внизу — минуты,
// треки, артисты.
//
// Рисуется в браузере на <canvas> и сразу отдаётся человеку файлом: сервер
// её не создаёт и не хранит. Обложки — те же адреса, что на странице (свой
// прокси картинок, тот же сайт), поэтому canvas можно сохранить в PNG.
//
// shareCardModel — что написано на карточке (чистая функция, под тесты);
// renderShareCard — рисует модель; fitText — обрезка строки с многоточием.

export const CARD_W = 1080;
export const CARD_H = 1920;
const TOP = 5; // мест в каждом топе

// Вёрстка — в «точках» макета 360×640, холст втрое больше.
const U = CARD_W / 360;
const PAD = 26;
const INNER = 360 - PAD * 2;
const DISPLAY = 'Unbounded, "Onest Variable", system-ui, sans-serif';
const TEXT = '"Onest Variable", system-ui, sans-serif';

/** Что написано на карточке: data — ответ /api/me/wrapped. */
export function shareCardModel(data, { lang = 'ru', username = '' } = {}) {
  const tr = (key, params) => translate(lang, key, params);
  const stat = (n, key) => ({ value: num(n, lang), label: word(n, key, lang) });
  return {
    year: data.year,
    username: username ? `@${username}` : '',
    title: tr('wrapped.cardTitle', { year: data.year }).split('\n'),
    songs: {
      label: tr('wrapped.cardSongs'),
      items: (data.top_tracks || []).slice(0, TOP).map((t) => ({
        title: t.title,
        sub: artistsOf(t)
          .map((a) => a.name)
          .join(', '),
        image: sized(t.cover_url, 300) || null,
      })),
    },
    albums: {
      label: tr('wrapped.topAlbums'),
      items: (data.top_albums || []).slice(0, TOP).map((a) => ({ title: a.title, image: sized(a.cover_url, 300) || null })),
    },
    artists: {
      label: tr('wrapped.cardArtists'),
      items: (data.top_artists || []).slice(0, TOP).map((a) => ({ title: a.name, image: sized(a.picture_url, 320) || null })),
    },
    stats: [stat(data.minutes, 'minutes'), stat(data.tracks, 'songs'), stat(data.artists, 'artists')],
  };
}

/** Строка, которая помещается в maxWidth: целиком или с «…» на конце. measure(text) → ширина. */
export function fitText(text, maxWidth, measure) {
  text = String(text ?? '');
  if (measure(text) <= maxWidth) return text;
  const chars = [...text]; // по символам, а не по половинкам эмодзи
  let lo = 0;
  let hi = chars.length;
  while (lo < hi) {
    const mid = Math.ceil((lo + hi) / 2);
    if (measure(chars.slice(0, mid).join('').trimEnd() + '…') <= maxWidth) lo = mid;
    else hi = mid - 1;
  }
  return lo ? chars.slice(0, lo).join('').trimEnd() + '…' : '';
}

// Картинка по адресу; не загрузилась — null (на её месте будет заглушка).
// Чужой сайт без CORS «пачкает» холст, и его уже не сохранить: такие
// просим с CORS — без него картинка просто не придёт.
function loadImage(url) {
  if (!url) return Promise.resolve(null);
  return new Promise((resolve) => {
    const img = new Image();
    if (new URL(url, location.href).origin !== location.origin) img.crossOrigin = 'anonymous';
    img.onload = () => resolve(img);
    img.onerror = () => resolve(null);
    img.src = url;
  });
}

function roundedPath(ctx, x, y, w, h, r) {
  r = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + w, y, x + w, y + h, r);
  ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r);
  ctx.arcTo(x, y, x + w, y, r);
  ctx.closePath();
}

// Картинка в квадрате со скруглением r (r = половина стороны — круг),
// обрезанная по центру, как object-fit: cover; нет картинки — тёмная плашка.
function drawArt(ctx, img, x, y, size, r) {
  ctx.save();
  roundedPath(ctx, x, y, size, size, r);
  ctx.clip();
  ctx.fillStyle = 'rgba(255, 255, 255, 0.12)';
  ctx.fillRect(x, y, size, size);
  if (img) {
    const w = img.naturalWidth || img.width;
    const h = img.naturalHeight || img.height;
    const side = Math.min(w, h);
    ctx.drawImage(img, (w - side) / 2, (h - side) / 2, side, side, x, y, size, size);
  }
  ctx.restore();
}

// Логотип: K, ветки которой — звуковые волны (как в шапке сайта, public/icon.svg).
function drawMark(ctx, x, y, size) {
  ctx.save();
  ctx.translate(x, y);
  ctx.scale(size / 100, size / 100);
  ctx.fillStyle = '#FF4D2E';
  ctx.fill(new Path2D('M30 0 H70 C88 0 100 12 100 30 V70 C100 88 88 100 70 100 H30 C12 100 0 88 0 70 V30 C0 12 12 0 30 0 Z'));
  ctx.strokeStyle = '#141414';
  ctx.lineWidth = 11;
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  ctx.stroke(new Path2D('M30 22 V78 M34 50 C45.3 51.9 51.9 47.2 54 36 C56.1 24.8 62.7 20.1 74 22 M34 50 C45.3 48.1 51.9 52.8 54 64 C56.1 75.2 62.7 79.9 74 78'));
  ctx.restore();
}

/** Рисует карточку; возвращает <canvas> 1080×1920. */
export async function renderShareCard(model) {
  const sections = [model.songs, model.albums, model.artists];
  // Шрифты сайта грузятся по наборам символов: просим их под текст карточки,
  // иначе первая отрисовка была бы запасным шрифтом.
  const sample = [model.username, ...model.title, ...sections.flatMap((s) => [s.label, ...s.items.flatMap((i) => [i.title, i.sub || ''])]), ...model.stats.map((s) => s.label), '0123456789'].join(' ');
  const fonts = [`800 27px ${DISPLAY}`, `700 13px ${DISPLAY}`, `700 13px ${TEXT}`, `400 13px ${TEXT}`];
  const [images] = await Promise.all([
    Promise.all(sections.map((s) => Promise.all(s.items.map((i) => loadImage(i.image))))),
    Promise.all(fonts.map((f) => document.fonts.load(f, sample).catch(() => {}))),
  ]);

  const canvas = document.createElement('canvas');
  canvas.width = CARD_W;
  canvas.height = CARD_H;
  const ctx = canvas.getContext('2d');
  ctx.scale(U, U);
  ctx.textBaseline = 'top';
  const spacing = (em, px) => 'letterSpacing' in ctx && (ctx.letterSpacing = `${em * px}px`);
  const font = (weight, px, family = TEXT, tracking = 0) => {
    ctx.font = `${weight} ${px}px ${family}`;
    spacing(tracking, px);
  };
  const measure = (s) => ctx.measureText(s).width;
  const text = (s, x, y, maxWidth, align = 'left') => {
    ctx.textAlign = align;
    ctx.fillText(maxWidth ? fitText(s, maxWidth, measure) : s, x, y);
  };
  const white = (alpha = 1) => (ctx.fillStyle = `rgba(255, 255, 255, ${alpha})`);

  // Фон: тёмный, с фиолетовым пятном из левого верхнего угла (цвет страницы итогов).
  ctx.fillStyle = '#121212';
  ctx.fillRect(0, 0, 360, 640);
  ctx.save();
  ctx.translate(72, 0);
  ctx.scale(1, 384 / 432);
  const glow = ctx.createRadialGradient(0, 0, 0, 0, 0, 432);
  glow.addColorStop(0, '#7a1bd0');
  glow.addColorStop(0.38, '#3a0f6b');
  glow.addColorStop(0.72, '#121212');
  ctx.fillStyle = glow;
  ctx.fillRect(-72, 0, 360, 640 * (432 / 384));
  ctx.restore();

  // Шапка: логотип слева, ник справа.
  drawMark(ctx, PAD, PAD, 22);
  white();
  font(700, 13, DISPLAY);
  text('kaikku', PAD + 29, PAD + 4);
  white(0.85);
  text(model.username, 360 - PAD, PAD + 4, INNER - 110, 'right');

  let y = 62;
  white();
  font(800, 27, DISPLAY, -0.03);
  for (const line of model.title) {
    text(line, PAD, y, INNER);
    y += 28.5;
  }
  y += 16;

  const kicker = (label) => {
    white(0.8);
    font(700, 10.5, TEXT, 0.09);
    text(label.toUpperCase(), PAD, y, INNER);
    y += 21;
  };

  // Треки: номер, обложка, название и исполнители.
  if (model.songs.items.length) {
    kicker(model.songs.label);
    model.songs.items.forEach((item, i) => {
      white(0.75);
      font(700, 12);
      text(String(i + 1), PAD + 6, y + 11, 0, 'center');
      drawArt(ctx, images[0][i], PAD + 22, y, 36, 5);
      white();
      font(700, 13.5);
      text(item.title, PAD + 68, y + 3, INNER - 68);
      white(0.75);
      font(400, 11.5);
      text(item.sub, PAD + 68, y + 20, INNER - 68);
      y += 43;
    });
    y += 5;
  }

  // Альбомы и артисты: ряд из пяти картинок с подписями.
  const strip = (section, imgs, round) => {
    if (!section.items.length) return;
    kicker(section.label);
    const gap = 8;
    const size = (INNER - gap * (TOP - 1)) / TOP;
    section.items.forEach((item, i) => {
      const x = PAD + i * (size + gap);
      drawArt(ctx, imgs[i], x, y, size, round ? size / 2 : 6);
      white();
      font(600, 10.5);
      text(item.title, x + size / 2, y + size + 6, size, 'center');
    });
    y += size + 6 + 13 + 14;
  };
  strip(model.albums, images[1], false);
  strip(model.artists, images[2], true);

  // Подвал: «14 873 минуты · 1 204 трека · 311 исполнителей», числа жирнее.
  let x = PAD;
  ctx.textAlign = 'left';
  model.stats.forEach((s, i) => {
    const parts = [
      [800, `${s.value} `],
      [500, s.label + (i < model.stats.length - 1 ? ' · ' : '')],
    ];
    for (const [weight, str] of parts) {
      white(weight === 800 ? 1 : 0.85);
      font(weight, 12);
      ctx.fillText(str, x, 640 - 22 - 14);
      x += measure(str);
    }
  });

  return canvas;
}

/** PNG карточки (Blob). */
export async function shareCardBlob(model) {
  const canvas = await renderShareCard(model);
  return new Promise((resolve, reject) => canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('toBlob failed'))), 'image/png'));
}
