import { test } from 'node:test';
import assert from 'node:assert/strict';
import { markLastfmLink, takeLastfmLink } from '../src/lib/lastfmLink.js';

function memory() {
  const m = new Map();
  return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, v), removeItem: (k) => m.delete(k) };
}

test('a token is accepted only after this browser started linking', () => {
  const s = memory();
  assert.equal(takeLastfmLink(s, 1000), false);
  markLastfmLink(s, 1000);
  assert.equal(takeLastfmLink(s, 2000), true);
  assert.equal(takeLastfmLink(s, 2000), false, 'one use');
});

test('an old start does not count', () => {
  const s = memory();
  markLastfmLink(s, 0);
  assert.equal(takeLastfmLink(s, 31 * 60 * 1000), false);
});

test('storage errors refuse the token', () => {
  const broken = { getItem() { throw new Error('blocked'); }, setItem() { throw new Error('blocked'); }, removeItem() {} };
  markLastfmLink(broken, 0);
  assert.equal(takeLastfmLink(broken, 0), false);
});
