import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sortRows, sortLibrary } from '../src/lib/sort.js';

const row = (title, artist, extra = {}) => ({ title, artists: [{ name: artist }], album: { title: extra.album || '' }, ...extra });

test('songs sort by title (ё as е, numbers as numbers), stable, empty last', () => {
  const rows = [row('Ёлка', 'A'), row('Track 10', 'B'), row('Track 2', 'C'), row('Елка', 'D'), row('', 'E')];
  assert.deepEqual(
    sortRows(rows, 'title', false, 'ru').map((r) => r.artists[0].name),
    ['A', 'D', 'C', 'B', 'E'],
  );
  assert.equal(sortRows(rows, 'custom'), rows, 'свой порядок — как есть');
});

test('date added and duration, descending', () => {
  const rows = [row('a', 'x', { addedAt: '2026-01-01T00:00:00Z', durationMs: 1 }), row('b', 'x', { addedAt: '2026-03-01T00:00:00Z', durationMs: 3 }), row('c', 'x', {})];
  assert.deepEqual(sortRows(rows, 'added', true).map((r) => r.title), ['b', 'a', 'c']);
  assert.deepEqual(sortRows(rows, 'duration', false).map((r) => r.title), ['a', 'b', 'c']);
});

test('library: recent counts plays and additions, added only additions, alpha by name', () => {
  const items = [
    { key: 'album:1', name: 'Zeta' },
    { key: 'artist:2', name: 'alpha' },
    { key: 'playlist:3', name: 'Mid' },
  ];
  const times = {
    added: { 'album:1': '2026-01-01T00:00:00Z', 'artist:2': '2026-02-01T00:00:00Z', 'playlist:3': '2026-03-01T00:00:00Z' },
    played: { 'album:1': '2026-04-01T00:00:00Z' },
  };
  assert.deepEqual(sortLibrary(items, 'recent', times).map((x) => x.key), ['album:1', 'playlist:3', 'artist:2']);
  assert.deepEqual(sortLibrary(items, 'added', times).map((x) => x.key), ['playlist:3', 'artist:2', 'album:1']);
  assert.deepEqual(sortLibrary(items, 'alpha', times).map((x) => x.name), ['alpha', 'Mid', 'Zeta']);
});
