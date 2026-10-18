import { sized } from './imageproxy.js';
import { LRU } from './lru.js';

// Динамические цвета из обложек — как у Spotify/Apple Music: шапка страницы
// альбома/артиста и плеер окрашиваются в цвет картинки.
//
// Картинка уменьшается до 32×32 на canvas. Цветные пиксели раскладываются
// по секторам цветового круга (24 по 15°), вес пикселя — его насыщенность
// (хрома): так голубые ветки на бледном фоне дают голубой, а не серый, хотя
// фона больше. Побеждает окно из трёх соседних секторов с наибольшим весом
// (оттенок, разбитый на два сектора, не проигрывает), цвет — средний по
// нему с теми же весами. Если цветных пикселей почти нет — обложка
// серая/чёрно-белая, и палитра тоже серая.
// Картинки Deezer идут через наш /api/img, свои обложки — /media: всё с
// того же origin, так что canvas читается без проблем.

const SECTORS = 24;
const MIN_CHROMA = 0.05; // ниже — считаем пиксель серым
// Сколько «цветного веса» (сумма хромы в лучшем окне на пиксель) нужно,
// чтобы обложка считалась цветной. Подобрано на реальных обложках: у
// бледно-голубой акварели ~0.049, у тёмной цветной обложки ~0.015, у
// чёрно-серебряной Discovery ~0.007.
const COLOURFUL_SHARE = 0.012;

// Пиксели по секторам цветового круга. w — сумма хромы (по ней выбирается
// оттенок), r/g/b — суммы с весом хрома², q — сумма хрома²: средний цвет
// тянется к самым ярким пикселям. all — простая сумма по всем пикселям.
function sectorize(data) {
  const sectors = Array.from({ length: SECTORS }, () => ({ w: 0, q: 0, r: 0, g: 0, b: 0 }));
  const all = { r: 0, g: 0, b: 0, n: 0 };
  for (let i = 0; i + 3 < data.length; i += 4) {
    if (data[i + 3] < 128) continue;
    const r = data[i];
    const g = data[i + 1];
    const b = data[i + 2];
    all.r += r;
    all.g += g;
    all.b += b;
    all.n++;
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    const chroma = (max - min) / 255;
    const l = (max + min) / 510;
    if (chroma < MIN_CHROMA || l < 0.06 || l > 0.94) continue;
    const { h } = rgbToHsl({ r, g, b });
    const q = chroma * chroma;
    const sec = sectors[Math.floor(h / (360 / SECTORS)) % SECTORS];
    sec.w += chroma;
    sec.q += q;
    sec.r += r * q;
    sec.g += g * q;
    sec.b += b * q;
  }
  return { sectors, all };
}

// Окно из трёх соседних секторов с наибольшим весом: { i, w }.
function bestWindow(sectors) {
  let best = -1;
  let bestW = 0;
  for (let i = 0; i < SECTORS; i++) {
    const w = sectors[(i + SECTORS - 1) % SECTORS].w + sectors[i].w + sectors[(i + 1) % SECTORS].w;
    if (w > bestW) {
      bestW = w;
      best = i;
    }
  }
  return { i: best, w: bestW };
}

// Средний цвет окна вокруг сектора i.
function windowColour(sectors, i) {
  const acc = { q: 0, r: 0, g: 0, b: 0 };
  for (const d of [-1, 0, 1]) {
    const sec = sectors[(i + d + SECTORS) % SECTORS];
    acc.q += sec.q;
    acc.r += sec.r;
    acc.g += sec.g;
    acc.b += sec.b;
  }
  return { r: Math.round(acc.r / acc.q), g: Math.round(acc.g / acc.q), b: Math.round(acc.b / acc.q) };
}

/** @param {ArrayLike<number>} data RGBA, как ImageData.data */
export function pickColor(data) {
  const { sectors, all } = sectorize(data);
  if (!all.n) return null;
  const { i: best, w: bestW } = bestWindow(sectors);
  if (best < 0 || bestW < COLOURFUL_SHARE * all.n) {
    // Серая обложка: средняя яркость без оттенка.
    const v = Math.round((all.r + all.g + all.b) / (3 * all.n));
    return { r: v, g: v, b: v };
  }
  return windowColour(sectors, best);
}

/**
 * Основные цвета обложки для переливающегося фона полноэкранного плеера:
 * пиксели делятся на k групп похожих цветов (k-means), возвращаются
 * средние цвета групп с их долей n (0…1), крупные первыми. В отличие от
 * pickColor, тут и серые, и тёмные области: у тёмной обложки с фиолетовыми
 * прядями — тёмно-фиолетовый, тёмно-коричневый, серый, а не один серый.
 * Группы ищутся по цвету с оттенком, усиленным в CHROMA_GAIN раз: иначе
 * тёмные пиксели разного оттенка сливаются в одну группу по яркости.
 */
const CHROMA_GAIN = 4;

export function clusterColours(data, k = 6) {
  const orig = [];
  const px = [];
  for (let i = 0; i + 3 < data.length; i += 4) {
    if (data[i + 3] < 128) continue;
    const c = [data[i], data[i + 1], data[i + 2]];
    const m = (c[0] + c[1] + c[2]) / 3;
    orig.push(c);
    px.push(c.map((v) => m + CHROMA_GAIN * (v - m)));
  }
  if (!px.length) return [];
  // Начальные центры — пиксели на равных шагах по яркости: детерминированно
  // и сразу от тёмных до светлых.
  const byLum = [...px].sort((x, y) => x[0] + x[1] + x[2] - (y[0] + y[1] + y[2]));
  let centres = Array.from({ length: k }, (_, i) => [...byLum[Math.floor(((i + 0.5) * byLum.length) / k)]]);
  const owner = new Array(px.length).fill(0);
  for (let iter = 0; iter < 10; iter++) {
    for (let j = 0; j < px.length; j++) {
      let best = 0;
      let bestD = Infinity;
      for (let c = 0; c < centres.length; c++) {
        const d = (px[j][0] - centres[c][0]) ** 2 + (px[j][1] - centres[c][1]) ** 2 + (px[j][2] - centres[c][2]) ** 2;
        if (d < bestD) (bestD = d), (best = c);
      }
      owner[j] = best;
    }
    const sums = centres.map(() => [0, 0, 0, 0]);
    for (let j = 0; j < px.length; j++) {
      const t = sums[owner[j]];
      t[0] += px[j][0];
      t[1] += px[j][1];
      t[2] += px[j][2];
      t[3]++;
    }
    centres = sums.map((t, c) => (t[3] ? [t[0] / t[3], t[1] / t[3], t[2] / t[3], t[3]] : [...centres[c].slice(0, 3), 0]));
  }
  // Цвет группы — среднее настоящих пикселей, без усиления.
  const sums = centres.map(() => [0, 0, 0, 0]);
  for (let j = 0; j < orig.length; j++) {
    const t = sums[owner[j]];
    t[0] += orig[j][0];
    t[1] += orig[j][1];
    t[2] += orig[j][2];
    t[3]++;
  }
  return sums
    .filter((t) => t[3] > 0)
    .map(([r, g, b, n]) => ({ r: Math.round(r / n), g: Math.round(g / n), b: Math.round(b / n), n: n / orig.length }))
    .sort((x, y) => y.n - x.n);
}

export function rgbToHsl({ r, g, b }) {
  r /= 255;
  g /= 255;
  b /= 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  if (max === min) return { h: 0, s: 0, l };
  const d = max - min;
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  let h;
  if (max === r) h = (g - b) / d + (g < b ? 6 : 0);
  else if (max === g) h = (b - r) / d + 2;
  else h = (r - g) / d + 4;
  return { h: h * 60, s, l };
}

/** 'hsl(250 20% 31%)' (как пишет hsl() ниже) → { r, g, b }; не разобрали — серый. */
export function hslToRgb(str) {
  const m = /hsl\((\d+(?:\.\d+)?) (\d+(?:\.\d+)?)% (\d+(?:\.\d+)?)%\)/.exec(str || '');
  if (!m) return { r: 40, g: 40, b: 40 };
  const h = Number(m[1]) / 360;
  const s = Number(m[2]) / 100;
  const l = Number(m[3]) / 100;
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const ch = (t) => {
    t = (t + 1) % 1;
    const v = t < 1 / 6 ? p + (q - p) * 6 * t : t < 1 / 2 ? q : t < 2 / 3 ? p + (q - p) * (2 / 3 - t) * 6 : p;
    return Math.round(v * 255);
  };
  return { r: ch(h + 1 / 3), g: ch(h), b: ch(h - 1 / 3) };
}

const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v));
const hsl = (h, s, l) => `hsl(${Math.round(h)} ${Math.round(s * 100)}% ${Math.round(l * 100)}%)`;

export const DEFAULT_PALETTE = {
  base: 'hsl(0 0% 32%)',
  dark: 'hsl(0 0% 16%)',
  accent: 'hsl(0 0% 91%)',
  accentText: '#121212',
  header: 'hsl(0 0% 33%)',
  page: 'hsl(0 0% 11%)',
  mesh: ['hsl(0 0% 24%)', 'hsl(0 0% 14%)', 'hsl(0 0% 32%)', 'hsl(0 0% 18%)'],
};

/**
 * Палитра из цвета обложки:
 *  base   — плашка трека в плеере;
 *  dark   — фон плеера/полноэкранного режима;
 *  accent — кнопка Play, полоса прогресса, галочки; всегда светлый, чтобы
 *           читался на тёмном фоне. У серых обложек — почти белый;
 *  header — шапка страницы, как у Spotify: насыщенный цвет средней яркости
 *           (к низу шапки затемняется и уходит в фон — App.svelte, .tint);
 *  page   — вся страница, как у Apple Music: ровный глубокий цвет, темнее
 *           и насыщеннее обложки (белый текст читается); у серых обложек —
 *           почти чёрный. Оттенок тот же, что у шапки и плеера;
 *  mesh   — 4 цвета переливающегося фона полноэкранного плеера: главный
 *           цвет и другие цвета обложки, в том числе тёмные и серые
 *           (clusters — clusterColours, meshFrom).
 */
export function paletteFrom(rgb, clusters = []) {
  if (!rgb) return DEFAULT_PALETTE;
  let { h, s } = rgbToHsl(rgb);
  let grey = s < 0.12;
  // Тёмная блёклая обложка (тёмно-синий фон, фиолетовые пряди) ярких
  // цветов не даёт, но она и не серая: как у Apple Music, берём её
  // преобладающий тёмный оттенок — приглушённо, без подкрашивания.
  const tint = grey ? dominantTint(clusters) : null;
  if (tint) {
    ({ h, s } = tint);
    grey = false;
    return {
      base: hsl(h, clamp(s, 0.2, 0.35), 0.24),
      dark: hsl(h, clamp(s, 0.15, 0.3), 0.12),
      accent: hsl(h, 0.3, 0.82),
      accentText: '#121212',
      header: hsl(h, clamp(s, 0.2, 0.35), 0.28),
      page: hsl(h, clamp(s, 0.2, 0.4), 0.11),
      mesh: meshFrom({ h, s: clamp(s, 0.2, 0.4), l: 0.2 }, clusters),
    };
  }
  return {
    // Заметно насыщеннее самих пикселей обложки — у бледной акварели всё
    // равно выходит внятный синий, а не серо-голубой.
    base: hsl(h, grey ? 0 : clamp(s, 0.42, 0.62), 0.3),
    dark: hsl(h, grey ? 0 : clamp(s, 0.3, 0.5), 0.15),
    accent: grey ? hsl(0, 0, 0.91) : hsl(h, clamp(s, 0.55, 0.85), 0.66),
    accentText: '#121212',
    header: grey ? hsl(0, 0, 0.33) : hsl(h, clamp(s, 0.45, 0.75), 0.4),
    page: grey ? hsl(0, 0, 0.11) : hsl(h, clamp(s * 1.25, 0.55, 0.95), 0.18),
    mesh: meshFrom(grey ? null : { h, s: clamp(s * 1.2, 0.55, 0.85), l: 0.36 }, clusters),
  };
}

const chroma = (c) => (Math.max(c.r, c.g, c.b) - Math.min(c.r, c.g, c.b)) / 255;
// Тёмные цвета блёклые по природе: тёмно-фиолетовый — это rgb(40,35,55).
const tinted = (c) => chroma(c) >= 0.02 && rgbToHsl(c).s >= 0.15;

// Оттенок самой крупной группы цветов обложки, если она не серая и
// занимает хотя бы пятую часть картинки.
function dominantTint(clusters) {
  const c = clusters.find((x) => x.n >= 0.2);
  return c && tinted(c) ? rgbToHsl(c) : null;
}

// Пятна фона: не темнее 13% (иначе на чёрном почти не видно движения) и
// не светлее 42% — белый текст поверх должен читаться.
const MESH_MIN_L = 0.13;
const MESH_MAX_L = 0.42;

/**
 * 4 цвета фона полноэкранного плеера. Первый — цвет плеера (у цветной
 * обложки), дальше — группы цветов обложки (clusterColours): чем группа
 * больше и цветнее, тем раньше; почти одинаковые пропускаются. Цвет
 * насыщается и зажимается по яркости. Не хватило — варианты уже взятых.
 */
function meshFrom(main, clusters) {
  const picked = [];
  // Яркость — от яркости цвета на обложке: тёмная обложка даёт тёмный фон,
  // как у Apple Music, а не вытянутый до средне-серого.
  const norm = (c) => {
    const x = rgbToHsl(c);
    const col = tinted(c);
    return { h: col ? x.h : 0, s: col ? clamp(x.s * 1.3, 0.25, 0.8) : 0, l: clamp(x.l + 0.05, MESH_MIN_L, MESH_MAX_L) };
  };
  const similar = (a, b) => {
    const dh = Math.abs(((a.h - b.h + 540) % 360) - 180);
    const bothGrey = a.s === 0 && b.s === 0;
    return Math.abs(a.l - b.l) < 0.07 && (bothGrey || (a.s > 0 && b.s > 0 && dh < 25));
  };
  if (main) picked.push(main);
  const score = (c) => c.n * (0.4 + 6 * chroma(c));
  const ranked = [...clusters].sort((a, b) => score(b) - score(a));
  for (const c of ranked) {
    if (picked.length >= 4) break;
    // Пара точек не красит фон; яркое пятно (радуга на чёрном) — красит.
    if (c.n < 0.03 && !(c.n >= 0.008 && chroma(c) >= 0.25)) continue;
    const x = norm(c);
    if (!picked.some((y) => similar(x, y))) picked.push(x);
  }
  if (!picked.length) picked.push({ h: 0, s: 0, l: 0.24 });
  // Мало разных цветов: тот же цвет светлее/темнее и соседние оттенки.
  const variants = [
    [0, 0.12],
    [0, -0.12],
    [25, 0.06],
    [-25, -0.06],
  ];
  for (let i = 0; i < picked.length && picked.length < 4; i++) {
    const base = picked[i];
    for (const [dh, dl] of variants) {
      if (picked.length >= 4) break;
      const x = { h: (base.h + (base.s ? dh : 0) + 360) % 360, s: base.s, l: clamp(base.l + dl, MESH_MIN_L, MESH_MAX_L) };
      if (!picked.some((y) => similar(x, y))) picked.push(x);
    }
  }
  while (picked.length < 4) picked.push(picked[picked.length % 2]);
  // Пятна почти одной яркости (тёмная обложка) сливаются, и движения не
  // видно: разводим их по яркости вокруг среднего.
  const ls = picked.slice(0, 4).map((c) => c.l);
  if (Math.max(...ls) - Math.min(...ls) < 0.16) {
    const mid = clamp(ls.reduce((a, b) => a + b, 0) / ls.length, MESH_MIN_L + 0.08, MESH_MAX_L - 0.1);
    [0.1, -0.08, 0.03, -0.05].forEach((d, i) => (picked[i] = { ...picked[i], l: clamp(mid + d, MESH_MIN_L, MESH_MAX_L) }));
  }
  return picked.slice(0, 4).map((c) => hsl(c.h, c.s, c.l));
}

const cache = new LRU(200, { pages: false });

/** Палитра для картинки по URL; кешируется, при ошибке — DEFAULT_PALETTE. */
export function paletteForImage(url) {
  if (!url) return Promise.resolve(DEFAULT_PALETTE);
  // Цвет всегда считается по одной и той же картинке 300×300 (её же
  // показывает шапка альбома): плеер грузит 64×64, а цвет должен совпадать.
  url = sized(url, 300);
  if (!cache.has(url)) cache.set(url, loadPalette(url));
  return cache.get(url);
}

async function loadPalette(url) {
  try {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.decoding = 'async';
    img.src = url;
    await img.decode();
    const size = 32;
    const canvas = document.createElement('canvas');
    canvas.width = size;
    canvas.height = size;
    const ctx = canvas.getContext('2d', { willReadFrequently: true });
    ctx.drawImage(img, 0, 0, size, size);
    const data = ctx.getImageData(0, 0, size, size).data;
    return paletteFrom(pickColor(data), clusterColours(data));
  } catch {
    return DEFAULT_PALETTE;
  }
}

/** Применить палитру как CSS-переменные с префиксом (--page-*, --player-*). */
export function paletteVars(prefix, p) {
  return (
    `--${prefix}-base: ${p.base}; --${prefix}-dark: ${p.dark}; --${prefix}-accent: ${p.accent}; --${prefix}-accent-text: ${p.accentText};` +
    `--${prefix}-header: ${p.header || p.base}; --${prefix}-flat: ${p.page || p.dark};`
  );
}
