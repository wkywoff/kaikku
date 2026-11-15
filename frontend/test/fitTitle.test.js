import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fitFontSize } from '../src/lib/fitTitle.js';

test('a short title gets the largest size', () => {
  // «5331»: 3 px ширины на 1 px кегля, колонка 800 px — влезло бы и 266 px.
  assert.equal(fitFontSize(800, 3, 36, 80), 80);
});

test('a title that does not fit at the largest size shrinks to one line', () => {
  // 12 px на 1 px кегля: в 800 px встаёт при 66 px (66,6 — вниз до целого).
  assert.equal(fitFontSize(800, 12, 36, 80), 66);
  // Та же строка в узкой колонке — мельче: считается ширина, а не символы.
  assert.equal(fitFontSize(500, 12, 36, 80), 41);
});

test('a title too long for one line stays at the smallest size and wraps', () => {
  assert.equal(fitFontSize(800, 40, 36, 80), 36);
});

test('nothing to measure yet gives the smallest size', () => {
  assert.equal(fitFontSize(0, 12, 36, 80), 36);
  assert.equal(fitFontSize(800, 0, 36, 80), 36);
  assert.equal(fitFontSize(NaN, NaN, 36, 80), 36);
});
