import { test } from 'node:test';
import assert from 'node:assert/strict';
import { trackCount, cardsPerRow } from '../src/lib/layout.js';

test('a row holds as many cards as the grid has columns', () => {
  assert.equal(trackCount('178.667px 178.667px 178.667px'), 3);
  assert.equal(trackCount('184px 184px'), 2);
  assert.equal(trackCount(' 200px '), 1);
});

test('a grid that is not laid out yet gives no column count', () => {
  assert.equal(trackCount('none'), 0);
  assert.equal(trackCount(''), 0);
  assert.equal(trackCount(undefined), 0);
  assert.equal(trackCount('repeat(auto-fill, minmax(max(176px, 10%), 1fr))'), 0);
});

test('without a grid a shelf shows two cards', () => {
  assert.equal(cardsPerRow(null), 2);
});
