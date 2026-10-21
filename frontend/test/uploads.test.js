import { test } from 'node:test';
import assert from 'node:assert/strict';
import { recordTypeFor, missingForPublish, naturalCompare, fullDate, prefillFromTags, createQueue, isAudioFile, discsOf, withSeparators } from '../src/lib/uploads.js';

const min = 60 * 1000;
const tracks = (n, each = 3 * min) => Array.from({ length: n }, () => ({ durationMs: each }));

test('recordTypeFor: by count and length, like Spotify', () => {
  assert.equal(recordTypeFor([]), 'single');
  assert.equal(recordTypeFor(tracks(1)), 'single');
  assert.equal(recordTypeFor(tracks(3)), 'single');
  assert.equal(recordTypeFor(tracks(4)), 'ep');
  assert.equal(recordTypeFor(tracks(6)), 'ep');
  assert.equal(recordTypeFor(tracks(7)), 'album');
  assert.equal(recordTypeFor(tracks(2, 16 * min)), 'album'); // полчаса и больше — альбом
  assert.equal(recordTypeFor(tracks(5, 7 * min)), 'album');
});

test('missingForPublish names every gap, and files still uploading', () => {
  const full = {
    title: 'Big Miss', artists: [{ id: 'k1' }], recordType: 'single', releaseDate: '2026-10-15', coverUrl: '/media/c.jpg',
    rows: [{ id: 'k2', title: 'Song' }],
  };
  assert.deepEqual(missingForPublish(full), []);
  assert.deepEqual(missingForPublish({ ...full, title: ' ', artists: [], coverUrl: '' }), ['title', 'artists', 'cover']);
  assert.deepEqual(missingForPublish({ ...full, rows: [] }), ['tracks']);
  assert.deepEqual(missingForPublish({ ...full, rows: [{ id: 'k2', title: '' }, { id: null, title: 'x.flac' }] }), ['track_titles', 'uploading']);
  // трек, добавленный с другого релиза, здесь не называют
  assert.deepEqual(missingForPublish({ ...full, rows: [{ id: '12345', title: '', other: true }] }), []);
});

test('discsOf numbers tracks by disc, skipping empty discs', () => {
  const sep = { sep: true };
  const rows = [sep, { id: 'a' }, { id: 'b' }, sep, sep, { id: 'c' }, sep];
  assert.deepEqual(
    discsOf(rows).map(({ row, disc, n }) => `${row.id}:${disc}.${n}`),
    ['a:1.1', 'b:1.2', 'c:2.1'],
  );
});

test('withSeparators puts a divider before each next disc', () => {
  let k = 0;
  const rows = withSeparators([{ id: 'a', disc: 1 }, { id: 'b', disc: 2 }, { id: 'c', disc: 2 }, { id: 'd', disc: 3 }], () => `s${++k}`);
  assert.deepEqual(rows.map((r) => r.id || r.key), ['a', 's1', 'b', 'c', 's2', 'd']);
});

test('naturalCompare puts 2 before 10', () => {
  assert.deepEqual(['10 - b.flac', '2 - a.flac', '1 - c.flac'].sort(naturalCompare), ['1 - c.flac', '2 - a.flac', '10 - b.flac']);
});

test('fullDate: only a whole, real date', () => {
  assert.equal(fullDate('2023-05-01'), '2023-05-01');
  assert.equal(fullDate('2023-05-01T10:00:00'), '2023-05-01');
  assert.equal(fullDate('2023'), '');
  assert.equal(fullDate('2023-02-30'), '');
  assert.equal(fullDate(undefined), '');
});

test('prefillFromTags fills only empty, untouched fields', () => {
  const tags = { album: 'Ash', date: '2023-05-01', artist: 'тихийпричал', album_artist: '' };
  assert.deepEqual(prefillFromTags({ title: '', artists: [] }, tags), { title: 'Ash', releaseDate: '2023-05-01', artistQuery: 'тихийпричал' });
  assert.deepEqual(prefillFromTags({ title: 'Mine', artists: [{ id: 'k1' }] }, tags, { releaseDate: true }), {});
  assert.deepEqual(prefillFromTags({ title: '', artists: [] }, { album: 'X' }, { title: true }), {});
  assert.deepEqual(prefillFromTags({ title: '' }, null), {});
});

test('isAudioFile: by type or extension', () => {
  assert.ok(isAudioFile({ type: '', name: 'a.FLAC' }));
  assert.ok(isAudioFile({ type: 'audio/mpeg', name: 'a' }));
  assert.ok(!isAudioFile({ type: 'image/png', name: 'cover.png' }));
});

test('createQueue uploads one at a time and retries a busy server', async () => {
  const calls = [];
  let busyOnce = true;
  const done = new Promise((resolve) => {
    const q = createQueue(
      async (item, onProgress) => {
        calls.push(item.file.name);
        onProgress(1);
        if (item.file.name === 'b' && busyOnce) {
          busyOnce = false;
          throw Object.assign(new Error('busy'), { status: 503, retryAfter: 1 });
        }
        if (item.file.name === 'c') throw Object.assign(new Error('not audio'), { status: 400 });
        return { track: { id: `k-${item.file.name}` } };
      },
      () => {
        if (!q.pending && q.items.length === 3) resolve(q);
      },
      { wait: async () => {} },
    );
    q.add([{ name: 'a' }, { name: 'b' }, { name: 'c' }]);
  });
  const q = await done;
  assert.deepEqual(calls, ['a', 'b', 'b', 'c']);
  assert.deepEqual(q.items.map((i) => i.state), ['done', 'done', 'error']);
  assert.equal(q.items[2].error, 'not audio');
  assert.equal(q.items[1].result.track.id, 'k-b');
});
