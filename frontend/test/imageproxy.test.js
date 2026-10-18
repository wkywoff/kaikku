import { test } from 'node:test';
import assert from 'node:assert/strict';
import { proxyImages } from '../src/lib/imageproxy.js';

test('proxyImages rewrites Deezer image links anywhere in a response', () => {
  const hash = '638e69b9caaf9f9f3f8826febea7b543';
  const got = proxyImages({
    artist: { name: 'Daft Punk', picture_url: `https://cdn-images.dzcdn.net/images/artist/${hash}/1000x1000-000000-80-0-0.jpg` },
    albums: [{ cover_url: `https://e-cdns-images.dzcdn.net/images/cover/${hash}/500x500-000000-80-0-0.jpg`, nb: 3 }],
    other: 'https://example.com/images/x.jpg',
    none: null,
  });
  assert.equal(got.artist.picture_url, `/api/img/artist/${hash}/1000x1000-000000-80-0-0.jpg`);
  assert.equal(got.albums[0].cover_url, `/api/img/cover/${hash}/500x500-000000-80-0-0.jpg`);
  assert.equal(got.albums[0].nb, 3);
  assert.equal(got.other, 'https://example.com/images/x.jpg');
  assert.equal(got.none, null);
});

test('sized picks the image size: Deezer by URL, downloaded covers via /api/cover', async () => {
  const { sized } = await import('../src/lib/imageproxy.js');
  const hash = '638e69b9caaf9f9f3f8826febea7b543';
  assert.equal(sized(`/api/img/cover/${hash}/1000x1000-000000-80-0-0.jpg`, 300), `/api/img/cover/${hash}/300x300-000000-80-0-0.jpg`);
  assert.equal(sized(`/api/img/artist/${hash}/250x250-000000-80-0-0.jpg`, 64), `/api/img/artist/${hash}/64x64-000000-80-0-0.jpg`);
  assert.equal(sized('/media/covers/3007dedab68318ab6e68b09ca79f17f7.jpg', 300), '/api/cover/300/covers/3007dedab68318ab6e68b09ca79f17f7.jpg');
  assert.equal(sized('/media/covers/artists/b224ceb79c858a8557b7ef1575c1df55.jpg', 64), '/api/cover/64/covers/artists/b224ceb79c858a8557b7ef1575c1df55.jpg');
  assert.equal(sized('blob:http://localhost/abc', 300), 'blob:http://localhost/abc');
  assert.equal(sized(null, 300), null);
});

test('srcsetOf offers the same picture in several sizes, or nothing to choose from', async () => {
  const { srcsetOf } = await import('../src/lib/imageproxy.js');
  const hash = '638e69b9caaf9f9f3f8826febea7b543';
  assert.equal(
    srcsetOf(`/api/img/cover/${hash}/1000x1000-000000-80-0-0.jpg`, [200, 300]),
    `/api/img/cover/${hash}/200x200-000000-80-0-0.jpg 200w, /api/img/cover/${hash}/300x300-000000-80-0-0.jpg 300w`,
  );
  // Уже 200×200 — всё равно набор, а не «не уменьшить».
  assert.equal(
    srcsetOf(`/api/img/artist/${hash}/200x200-000000-80-0-0.jpg`, [200, 320]),
    `/api/img/artist/${hash}/200x200-000000-80-0-0.jpg 200w, /api/img/artist/${hash}/320x320-000000-80-0-0.jpg 320w`,
  );
  assert.equal(
    srcsetOf('/media/covers/custom/3007dedab68318ab6e68b09ca79f17f7.jpg', [200, 300]),
    '/api/cover/200/covers/custom/3007dedab68318ab6e68b09ca79f17f7.jpg 200w, /api/cover/300/covers/custom/3007dedab68318ab6e68b09ca79f17f7.jpg 300w',
  );
  assert.equal(srcsetOf('blob:http://localhost/abc', [200, 300]), undefined);
  assert.equal(srcsetOf(null, [200, 300]), undefined);
});

test('artworkFor gives the lock screen three sizes of the cover', async () => {
  const { artworkFor } = await import('../src/lib/imageproxy.js');
  assert.deepEqual(artworkFor(null), []);
  const art = artworkFor('/media/covers/ab/cd.jpg');
  assert.deepEqual(art.map((a) => a.sizes), ['200x200', '300x300', '640x640']);
  assert.ok(art.every((a) => a.type === 'image/jpeg'));
});
