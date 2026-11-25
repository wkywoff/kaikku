import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pickColor, averageDown, clusterColours, paletteFrom, rgbToHsl, hslToRgb, DEFAULT_PALETTE } from '../src/lib/color.js';

function image(pixels) {
  const out = [];
  for (const [r, g, b, n] of pixels) for (let i = 0; i < n; i++) out.push(r, g, b, 255);
  return out;
}

test('a vivid spot beats a larger near-black background', () => {
  const rgb = pickColor(image([
    [8, 8, 10, 800],
    [200, 40, 60, 120],
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
    [225, 228, 232, 700],
    [60, 100, 150, 120],
    [90, 120, 170, 80],
    [20, 25, 30, 124],
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
    [200, 60, 60, 4],
  ]));
  assert.match(paletteFrom(rgb).base, /hsl\(0 0% /);
});

test('whole-page colour: deep and saturated, the same hue as the header and the player', () => {
  const red = paletteFrom(pickColor(image([[150, 20, 20, 700], [60, 5, 5, 324]])));
  const [, rh, rs, rl] = red.page.match(/hsl\((\d+) (\d+)% (\d+)%\)/).map(Number);
  assert.ok((rh <= 10 || rh >= 350) && rs >= 80 && rl <= 20, red.page);
  const bw = paletteFrom(pickColor(image([[15, 15, 15, 700], [220, 220, 220, 300], [30, 120, 200, 24]])));
  const hue = (c) => Number(c.match(/hsl\((\d+)/)[1]);
  assert.ok(!bw.header.includes(' 0% '), bw.header);
  assert.equal(hue(bw.page), hue(bw.header));
  assert.equal(hue(bw.page), hue(bw.base));
  assert.equal(paletteFrom(pickColor(image([[15, 15, 15, 700], [220, 220, 220, 300]]))).page, 'hsl(0 0% 11%)');
  const [, hh, hs, hl] = red.header.match(/hsl\((\d+) (\d+)% (\d+)%\)/).map(Number);
  assert.equal(hh, rh);
  assert.equal(hs, rs);
  assert.ok(hl > rl + 10, red.header);
});

test('header colour is bright where white text allows it and darker where it would not', () => {
  const header = (rgb) => paletteFrom(rgb).header;
  const light = (c) => Number(c.match(/ (\d+)%\)$/)[1]);
  const red = header({ r: 200, g: 10, b: 10 });
  const blue = header({ r: 20, g: 40, b: 200 });
  const yellow = header({ r: 220, g: 200, b: 20 });
  assert.ok(light(red) >= 34, red);
  assert.ok(light(blue) >= 38, blue);
  assert.ok(light(yellow) <= 26, yellow);
  const maroon = header({ r: 62, g: 15, b: 14 });
  assert.ok(light(maroon) >= 22 && light(maroon) <= 25, maroon);
});

test('clusterColours keeps dark tinted areas apart from dark warm ones', () => {
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
  const data = image([[14, 13, 22, 400], [26, 17, 15, 400], [70, 66, 72, 224]]);
  const dark = paletteFrom(pickColor(data), clusterColours(data));
  assert.ok(hue(dark.page) > 230 && hue(dark.page) < 270 && sat(dark.page) <= 40 && light(dark.page) <= 12, dark.page);
  assert.equal(hue(dark.base), hue(dark.page));
  assert.ok(dark.mesh.some((c) => sat(c) > 0 && hue(c) > 230 && hue(c) < 270), dark.mesh.join());
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

function canvasPixels(width, height, at) {
  const out = [];
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) out.push(...at(x, y));
  return out;
}

test('averageDown mixes every source pixel, so a noisy cover gives one stable colour', () => {
  const noisy = canvasPixels(300, 300, (x, y) => ((x + y) % 2 ? [0, 0, 255, 255] : [100, 100, 155, 255]));
  const shifted = canvasPixels(300, 300, (x, y) => ((x + y) % 2 ? [100, 100, 155, 255] : [0, 0, 255, 255]));
  const a = pickColor(averageDown(noisy, 300, 300, 32));
  const b = pickColor(averageDown(shifted, 300, 300, 32));
  for (const c of ['r', 'g', 'b']) assert.ok(Math.abs(a[c] - b[c]) <= 2, `${JSON.stringify(a)} ${JSON.stringify(b)}`);
  assert.ok(Math.abs(a.r - 50) <= 2 && Math.abs(a.b - 205) <= 2, JSON.stringify(a));
});

test('averageDown keeps the layout of sizes that do not divide evenly', () => {
  const halves = canvasPixels(50, 37, (x) => (x < 25 ? [200, 0, 0, 255] : [0, 0, 200, 255]));
  const out = averageDown(halves, 50, 37, 2);
  assert.deepEqual([...out], [200, 0, 0, 255, 0, 0, 200, 255, 200, 0, 0, 255, 0, 0, 200, 255]);
});

test('averageDown fills the grid from an image smaller than it', () => {
  const out = averageDown([10, 20, 30, 255], 1, 1, 4);
  assert.equal(out.length, 64);
  for (let i = 0; i < out.length; i += 4) assert.deepEqual([...out.slice(i, i + 4)], [10, 20, 30, 255]);
});

test('averageDown ignores the colour of transparent pixels', () => {
  const out = averageDown([200, 100, 0, 255, 0, 0, 255, 0], 2, 1, 1);
  assert.deepEqual([...out], [200, 100, 0, 128]);
  assert.deepEqual([...averageDown([0, 0, 255, 0], 1, 1, 1)], [0, 0, 0, 0]);
});
