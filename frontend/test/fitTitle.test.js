import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fitFontSize } from '../src/lib/fitTitle.js';

test('a short title gets the largest size', () => {
  assert.equal(fitFontSize(800, 3, 36, 80), 80);
});

test('a title that does not fit at the largest size shrinks to one line', () => {
  assert.equal(fitFontSize(800, 12, 36, 80), 66);
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
