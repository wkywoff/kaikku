import { test } from 'node:test';
import assert from 'node:assert/strict';
import { get } from 'svelte/store';

const storage = new Map([['kaikku_side_player', JSON.stringify({ open: true, width: 999 })]]);
global.localStorage = {
  getItem: (k) => (storage.has(k) ? storage.get(k) : null),
  setItem: (k, v) => storage.set(k, String(v)),
  removeItem: (k) => storage.delete(k),
};

const { clampWidth, maxWidthFor, parseSaved, playingFrom, sidePlayer, sideFits, SIDE_MIN, SIDE_MAX, SIDE_DEFAULT } = await import(
  '../src/lib/stores/sidePlayer.js'
);

const tr = (key) => ({ 'liked.title': 'Liked Songs' })[key] || key;

test('the panel width stays between the minimum and the maximum', () => {
  assert.equal(clampWidth(100), SIDE_MIN);
  assert.equal(clampWidth(5000), SIDE_MAX);
  assert.equal(clampWidth(333.6), 334);
  assert.equal(clampWidth('abc'), SIDE_DEFAULT);
  assert.equal(clampWidth(undefined), SIDE_DEFAULT);
});

test('a narrow window gives the panel at most 40% of its width, never under the minimum', () => {
  assert.equal(maxWidthFor(1000), 400);
  assert.equal(maxWidthFor(1920), SIDE_MAX);
  assert.equal(maxWidthFor(500), SIDE_MIN);
  assert.equal(clampWidth(420, 1000), 400);
});

test('a broken or foreign saved state opens nothing', () => {
  assert.deepEqual(parseSaved('{oops'), { open: false, width: SIDE_DEFAULT });
  assert.deepEqual(parseSaved(null), { open: false, width: SIDE_DEFAULT });
  assert.deepEqual(parseSaved(JSON.stringify({ open: 'yes', width: 10 })), { open: false, width: SIDE_MIN });
});

test('the panel comes back open with the saved width, clamped', () => {
  assert.deepEqual(get(sidePlayer), { open: true, width: SIDE_MAX });
});

test('closing is saved at once, dragging only when the drag ends', () => {
  sidePlayer.close();
  assert.equal(JSON.parse(storage.get('kaikku_side_player')).open, false);
  sidePlayer.resize(300);
  assert.equal(get(sidePlayer).width, 300);
  assert.equal(JSON.parse(storage.get('kaikku_side_player')).width, SIDE_MAX);
  sidePlayer.commitWidth();
  assert.equal(JSON.parse(storage.get('kaikku_side_player')).width, 300);
  sidePlayer.open();
  assert.deepEqual(JSON.parse(storage.get('kaikku_side_player')), { open: true, width: 300 });
});

test('without matchMedia the panel does not claim the window is wide enough', () => {
  assert.equal(get(sideFits), false);
});

test('the panel header names and links where the track was started from', () => {
  assert.deepEqual(playingFrom({ album: 'Album', context: { kind: 'album', id: '42' } }, tr), { title: 'Album', href: '/album/42' });
  assert.deepEqual(playingFrom({ context: { kind: 'playlist', id: 'p-1', title: 'Road' } }, tr), { title: 'Road', href: '/playlist/p-1' });
  assert.deepEqual(playingFrom({ context: { kind: 'liked', id: '' } }, tr), { title: 'Liked Songs', href: '/collection/tracks' });
  assert.deepEqual(playingFrom({ context: { kind: 'mix', id: 'on-repeat', title: 'On Repeat' } }, tr), { title: 'On Repeat', href: '/mix/on-repeat' });
  assert.equal(playingFrom({ context: { kind: 'artist', id: '7' } }, tr), null);
  assert.equal(playingFrom({ context: { kind: 'search', id: 'x', title: 'X' } }, tr), null);
  assert.equal(playingFrom({ context: null }, tr), null);
  assert.equal(playingFrom(null, tr), null);
});
