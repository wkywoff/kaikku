import { test } from 'node:test';
import assert from 'node:assert/strict';
import { countLiked, latestRelease } from '../src/lib/artistHighlights.js';

const NOW = new Date(2026, 10, 25, 14, 0);
const release = (id, release_date, more = {}) => ({ id, release_date, ...more });
const latestId = (albums, now = NOW) => latestRelease(albums, now)?.id ?? null;

test('the latest release is the newest one that is out, whatever the order of the list', () => {
  const albums = [release('spring', '2026-06-01'), release('autumn', '2026-10-30'), release('summer', '2026-08-14')];
  assert.equal(latestId(albums), 'autumn');
  assert.equal(latestId([...albums].reverse()), 'autumn');
});

test('of two releases of one day the first in the list is the latest', () => {
  assert.equal(latestId([release('a', '2026-10-30'), release('b', '2026-10-30')]), 'a');
});

test('a release older than six months is not shown', () => {
  assert.equal(latestId([release('edge', '2026-05-25')]), 'edge');
  assert.equal(latestId([release('old', '2026-05-24')]), null);
  assert.equal(latestId([]), null);
});

test('six months back from the end of a long month is the end of a short one', () => {
  const august = new Date(2026, 7, 31);
  assert.equal(latestId([release('feb', '2026-02-28')], august), 'feb');
  assert.equal(latestId([release('before', '2026-02-27')], august), null);
  assert.equal(latestId([release('last year', '2025-09-30')], new Date(2026, 2, 30)), 'last year');
});

test('a release comes out on its day, not before', () => {
  assert.equal(latestId([release('today', '2026-11-25'), release('tomorrow', '2026-11-26')]), 'today');
  assert.equal(latestId([release('tomorrow', '2026-11-26')]), null);
});

test('hidden releases and unknown dates are skipped', () => {
  const albums = [release('hidden', '2026-11-01', { hidden: true }), release('unknown', '0000-00-00'), release('none', undefined), release('seen', '2026-09-01')];
  assert.equal(latestId(albums), 'seen');
});

test('liked songs are counted once, and only while they are liked', () => {
  const liked = new Set(['1', '2', '3']);
  assert.equal(countLiked(liked, ['1', '2'], ['2', '3', '4']), 3);
  assert.equal(countLiked(liked, ['9'], []), 0);
  assert.equal(countLiked(new Set(), ['1'], ['2']), 0);
});
