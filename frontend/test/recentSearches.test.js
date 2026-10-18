import { test } from 'node:test';
import assert from 'node:assert/strict';

global.localStorage = { getItem: () => null, setItem() {}, removeItem() {} };
const { pushRecent, removeRecent } = await import('../src/lib/stores/recentSearches.js');

test('a search goes first, once, and the list is capped', () => {
  let l = pushRecent([], 'burya');
  l = pushRecent(l, 'Daft Punk');
  l = pushRecent(l, '  BURYA ');
  assert.deepEqual(l, ['BURYA', 'Daft Punk']);
  assert.deepEqual(pushRecent(l, '   '), l);
  let many = [];
  for (let i = 0; i < 20; i++) many = pushRecent(many, `q${i}`, 12);
  assert.equal(many.length, 12);
  assert.equal(many[0], 'q19');
  assert.deepEqual(removeRecent(l, 'burya'), ['Daft Punk']);
});
