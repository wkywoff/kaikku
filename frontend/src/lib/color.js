import { sized } from './imageproxy.js';
import { LRU } from './lru.js';

const SECTORS = 24;
const MIN_CHROMA = 0.05;
const COLOURFUL_SHARE = 0.012;

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

export function pickColor(data) {
  const { sectors, all } = sectorize(data);
  if (!all.n) return null;
  const { i: best, w: bestW } = bestWindow(sectors);
  if (best < 0 || bestW < COLOURFUL_SHARE * all.n) {
    const v = Math.round((all.r + all.g + all.b) / (3 * all.n));
    return { r: v, g: v, b: v };
  }
  return windowColour(sectors, best);
}

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

export function paletteFrom(rgb, clusters = []) {
  if (!rgb) return DEFAULT_PALETTE;
  let { h, s, l } = rgbToHsl(rgb);
  const grey = s < 0.12;
  const tint = grey ? dominantTint(clusters) : null;
  if (tint) {
    ({ h, s } = tint);
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
    base: hsl(h, grey ? 0 : clamp(s, 0.42, 0.62), 0.3),
    dark: hsl(h, grey ? 0 : clamp(s, 0.3, 0.5), 0.15),
    accent: grey ? hsl(0, 0, 0.91) : hsl(h, clamp(s, 0.55, 0.85), 0.66),
    accentText: '#121212',
    header: grey ? hsl(0, 0, 0.33) : headerTone(h, clamp(s * 1.25, 0.55, 0.95), l),
    page: grey ? hsl(0, 0, 0.11) : hsl(h, clamp(s * 1.25, 0.55, 0.95), 0.18),
    mesh: meshFrom(grey ? null : { h, s: clamp(s * 1.2, 0.55, 0.85), l: 0.36 }, clusters),
  };
}

const MAX_HEADER_LUMINANCE = 0.1;
const linear = (c) => {
  c /= 255;
  return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
};
const luminance = ({ r, g, b }) => 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(b);

function headerTone(h, s, coverL) {
  let l = Math.round(clamp(coverL * 100 + 10, 22, 40));
  while (l > 22 && luminance(hslToRgb(hsl(h, s, l / 100))) > MAX_HEADER_LUMINANCE) l--;
  return hsl(h, s, l / 100);
}

const chroma = (c) => (Math.max(c.r, c.g, c.b) - Math.min(c.r, c.g, c.b)) / 255;
const tinted = (c) => chroma(c) >= 0.02 && rgbToHsl(c).s >= 0.15;

function dominantTint(clusters) {
  const c = clusters.find((x) => x.n >= 0.2);
  return c && tinted(c) ? rgbToHsl(c) : null;
}

const MESH_MIN_L = 0.13;
const MESH_MAX_L = 0.42;

function meshFrom(main, clusters) {
  const picked = [];
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
    if (c.n < 0.03 && !(c.n >= 0.008 && chroma(c) >= 0.25)) continue;
    const x = norm(c);
    if (!picked.some((y) => similar(x, y))) picked.push(x);
  }
  if (!picked.length) picked.push({ h: 0, s: 0, l: 0.24 });
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
  const ls = picked.slice(0, 4).map((c) => c.l);
  if (Math.max(...ls) - Math.min(...ls) < 0.16) {
    const mid = clamp(ls.reduce((a, b) => a + b, 0) / ls.length, MESH_MIN_L + 0.08, MESH_MAX_L - 0.1);
    [0.1, -0.08, 0.03, -0.05].forEach((d, i) => (picked[i] = { ...picked[i], l: clamp(mid + d, MESH_MIN_L, MESH_MAX_L) }));
  }
  return picked.slice(0, 4).map((c) => hsl(c.h, c.s, c.l));
}

const cache = new LRU(200, { pages: false });

export function paletteForImage(url) {
  if (!url) return Promise.resolve(DEFAULT_PALETTE);
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

export function paletteVars(prefix, p) {
  return (
    `--${prefix}-base: ${p.base}; --${prefix}-dark: ${p.dark}; --${prefix}-accent: ${p.accent}; --${prefix}-accent-text: ${p.accentText};` +
    `--${prefix}-header: ${p.header || p.base}; --${prefix}-flat: ${p.page || p.dark};`
  );
}
