import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pickColor, clusterColours, paletteFrom, rgbToHsl, hslToRgb, DEFAULT_PALETTE } from '../src/lib/color.js';

function image(pixels) {
  // pixels: [[r,g,b,count], ...]
  const out = [];
  for (const [r, g, b, n] of pixels) for (let i = 0; i < n; i++) out.push(r, g, b, 255);
  return out;
}

test('a vivid spot beats a larger near-black background', () => {
  const rgb = pickColor(image([
    [8, 8, 10, 800], // фон обложки
    [200, 40, 60, 120], // красное пятно
  ]));
  assert.ok(rgb.r > 150 && rgb.g < 80, `got ${JSON.stringify(rgb)}`);
});

test('a saturated colour beats a larger grey area', () => {
  const rgb = pickColor(image([
    [120, 120, 120, 500],
    [40, 90, 200, 200],
  ]));
  assert.ok(rgb.b > 150, `got ${JSON.stringify(rgb)}`);
});

test('a monochrome cover yields a grey palette with a light accent', () => {
  const rgb = pickColor(image([[90, 90, 90, 300], [20, 20, 20, 300]]));
  const p = paletteFrom(rgb);
  assert.match(p.accent, /hsl\(0 0% 9\d%\)/);
  assert.match(p.base, /hsl\(0 0% /);
});

test('transparent / empty input falls back to the default palette', () => {
  assert.equal(pickColor([]), null);
  assert.equal(pickColor([255, 0, 0, 0]), null);
  assert.deepEqual(paletteFrom(null), DEFAULT_PALETTE);
});

test('palette keeps the hue and makes the accent readable on dark', () => {
  const p = paletteFrom({ r: 30, g: 60, b: 160 });
  const hue = Math.round(rgbToHsl({ r: 30, g: 60, b: 160 }).h);
  assert.ok(p.base.startsWith(`hsl(${hue} `), p.base);
  assert.ok(p.accent.endsWith('66%)'), p.accent);
});

test('a pale tinted cover gets its tint, not grey (thin blue lines on a pale background)', () => {
  const rgb = pickColor(image([
    [225, 228, 232, 700], // бледный фон
    [60, 100, 150, 120], // голубые ветки
    [90, 120, 170, 80],
    [20, 25, 30, 124], // тёмные контуры
  ]));
  const p = paletteFrom(rgb);
  const hue = Number(p.base.match(/hsl\((\d+)/)[1]);
  assert.ok(hue > 195 && hue < 225, p.base);
  assert.ok(!p.base.includes(' 0% '), `должен быть цветной: ${p.base}`);
});

test('a black-and-silver cover stays grey even with a few coloured specks', () => {
  const rgb = pickColor(image([
    [10, 10, 10, 800],
    [180, 182, 185, 210],
    [200, 60, 60, 4], // пара цветных точек
  ]));
  assert.match(paletteFrom(rgb).base, /hsl\(0 0% /);
});

test('whole-page colour: deep and saturated, the same hue as the header and the player', () => {
  const red = paletteFrom(pickColor(image([[150, 20, 20, 700], [60, 5, 5, 324]])));
  const [, rh, rs, rl] = red.page.match(/hsl\((\d+) (\d+)% (\d+)%\)/).map(Number);
  assert.ok((rh <= 10 || rh >= 350) && rs >= 80 && rl <= 20, red.page);
  // Чёрно-белая обложка с цветной деталью: шапка и плеер цветные — и фон
  // страницы тоже, а не серый (иначе режимы окраски не совпадают с плеером).
  const bw = paletteFrom(pickColor(image([[15, 15, 15, 700], [220, 220, 220, 300], [30, 120, 200, 24]])));
  const hue = (c) => Number(c.match(/hsl\((\d+)/)[1]);
  assert.ok(!bw.header.includes(' 0% '), bw.header);
  assert.equal(hue(bw.page), hue(bw.header));
  assert.equal(hue(bw.page), hue(bw.base));
  // Совсем серая — почти чёрный фон.
  assert.equal(paletteFrom(pickColor(image([[15, 15, 15, 700], [220, 220, 220, 300]]))).page, 'hsl(0 0% 11%)');
  // Шапка ярче фона страницы, как у Spotify.
  assert.match(red.header, / 40%\)$/);
});

test('clusterColours keeps dark tinted areas apart from dark warm ones', () => {
  // Тёмная обложка: тёмно-фиолетовый фон, тёмная кожа, немного серого.
  const cl = clusterColours(image([
    [14, 13, 22, 400],
    [26, 17, 15, 400],
    [70, 66, 72, 224],
  ]));
  const hues = cl.map((c) => Math.round(rgbToHsl(c).h));
  assert.ok(hues.some((h) => h > 230 && h < 270), `есть фиолетовый: ${hues}`);
  assert.ok(hues.some((h) => h < 30), `есть тёплый: ${hues}`);
  assert.ok(Math.abs(cl.reduce((a, c) => a + c.n, 0) - 1) < 1e-9);
  assert.deepEqual(clusterColours([]), []);
});

test('mesh: four distinct colours, the player colour first, dark tints kept, never too light', () => {
  const hue = (c) => Number(c.match(/hsl\((\d+)/)[1]);
  const sat = (c) => Number(c.match(/ (\d+)% /)[1]);
  const light = (c) => Number(c.match(/(\d+)%\)$/)[1]);
  const red = { r: 200, g: 30, b: 40 };
  const two = paletteFrom(red, clusterColours(image([[200, 30, 40, 600], [30, 60, 200, 424]])));
  assert.equal(two.mesh.length, 4);
  assert.equal(hue(two.mesh[0]), hue(two.base));
  assert.ok(two.mesh.some((c) => hue(c) > 220 && hue(c) < 240), two.mesh.join());
  assert.equal(new Set(two.mesh).size, 4, two.mesh.join());
  // Тёмная блёклая обложка (тёмно-фиолетовый фон): как у Apple Music —
  // приглушённый тёмно-фиолетовый, а не серый, и в фоне плеера он тоже есть.
  const data = image([[14, 13, 22, 400], [26, 17, 15, 400], [70, 66, 72, 224]]);
  const dark = paletteFrom(pickColor(data), clusterColours(data));
  assert.ok(hue(dark.page) > 230 && hue(dark.page) < 270 && sat(dark.page) <= 40 && light(dark.page) <= 12, dark.page);
  assert.equal(hue(dark.base), hue(dark.page));
  assert.ok(dark.mesh.some((c) => sat(c) > 0 && hue(c) > 230 && hue(c) < 270), dark.mesh.join());
  // Обложка в одном цвете: всё равно 4 разных пятна.
  const one = paletteFrom({ r: 30, g: 60, b: 200 }, clusterColours(image([[30, 60, 200, 1024]])));
  assert.equal(new Set(one.mesh).size, 4, one.mesh.join());
  for (const c of [...two.mesh, ...dark.mesh, ...one.mesh]) assert.ok(light(c) >= 13 && light(c) <= 42, c);
});

test('hslToRgb reads the palette strings back', () => {
  assert.deepEqual(hslToRgb('hsl(0 100% 50%)'), { r: 255, g: 0, b: 0 });
  assert.deepEqual(hslToRgb('hsl(240 100% 25%)'), { r: 0, g: 0, b: 128 });
  assert.deepEqual(hslToRgb('hsl(0 0% 20%)'), { r: 51, g: 51, b: 51 });
  const back = rgbToHsl(hslToRgb('hsl(250 20% 31%)'));
  assert.ok(Math.abs(back.h - 250) < 2 && Math.abs(back.l - 0.31) < 0.01, JSON.stringify(back));
  assert.deepEqual(hslToRgb('nonsense'), { r: 40, g: 40, b: 40 });
});
