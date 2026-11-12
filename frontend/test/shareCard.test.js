import { test } from 'node:test';
import assert from 'node:assert/strict';
import { shareCardModel, fitText } from '../src/lib/shareCard.js';

const data = {
  year: 2026,
  minutes: 14873,
  tracks: 1204,
  artists: 311,
  top_tracks: Array.from({ length: 7 }, (_, i) => ({ id: String(i), title: `Song ${i + 1}`, artist: 'A', artist_id: '1', cover_url: `/api/img/cover/${'a'.repeat(32)}/1000x1000-000000-80-0-0.jpg` })),
  top_albums: Array.from({ length: 10 }, (_, i) => ({ id: String(i), title: `Album ${i + 1}`, cover_url: '' })),
  top_artists: [{ id: '1', name: 'Only One', picture_url: '/media/covers/x.jpg' }],
};

test('share card shows five of each top, in the interface language', () => {
  const m = shareCardModel(data, { lang: 'en', username: 'ann' });
  assert.deepEqual(m.title, ['My 2026', 'in music']);
  assert.equal(m.username, '@ann');
  assert.equal(m.songs.label, 'Top songs');
  assert.deepEqual(m.songs.items.map((i) => i.title), ['Song 1', 'Song 2', 'Song 3', 'Song 4', 'Song 5']);
  assert.equal(m.songs.items[0].sub, 'A');
  assert.match(m.songs.items[0].image, /\/300x300-/); // обложка — уменьшенная, а не 1000×1000
  assert.equal(m.albums.items.length, 5);
  assert.equal(m.albums.items[0].image, null); // без обложки — заглушка
  assert.equal(m.artists.items.length, 1);
  assert.deepEqual(m.stats, [
    { value: '14,873', label: 'minutes' },
    { value: '1,204', label: 'songs' },
    { value: '311', label: 'artists' },
  ]);

  const ru = shareCardModel({ ...data, tracks: 2 }, { lang: 'ru' });
  assert.deepEqual(ru.title, ['Мой 2026', 'в музыке']);
  assert.equal(ru.username, '');
  assert.equal(ru.stats[1].label, 'трека');
  assert.equal(ru.artists.label, 'Топ артистов');
});

test('a year with nothing in a top leaves that list empty', () => {
  const m = shareCardModel({ year: 2025, minutes: 0, tracks: 0, artists: 0 }, { lang: 'en' });
  assert.deepEqual([m.songs.items, m.albums.items, m.artists.items], [[], [], []]);
});

test('fitText cuts a long line to the width with an ellipsis', () => {
  const measure = (s) => [...s].length * 10; // каждый символ — 10 px
  assert.equal(fitText('Short', 100, measure), 'Short');
  assert.equal(fitText('A very long album title', 100, measure), 'A very lo…');
  assert.equal(fitText('Word then space', 60, measure), 'Word…'); // без пробела перед «…»
  assert.equal(fitText('Привет, мир', 50, measure), 'Прив…');
  assert.equal(fitText('abc', 5, measure), '');
  assert.equal(fitText(undefined, 50, measure), '');
});
