import { test } from 'node:test';
import assert from 'node:assert/strict';
import { trackIdFrom, albumIdFrom, artistIdFrom, isLocalId } from '../src/lib/links.js';

test('trackIdFrom: Deezer and kaikku links, a bare ID', () => {
  assert.equal(trackIdFrom(' 491201822 '), '491201822');
  assert.equal(trackIdFrom('https://www.deezer.com/en/track/491201822?utm=x'), '491201822');
  assert.equal(trackIdFrom('https://music.example.com/album/62145332?track=491201822'), '491201822');
  assert.equal(trackIdFrom('https://music.example.com/album/62145332?from=x&track=491201822'), '491201822');
  assert.equal(trackIdFrom('https://music.example.com/album/62145332'), null); // альбом, не трек
  assert.equal(trackIdFrom('Тихий вечер'), null);
});

test('own uploads: k-IDs in links and bare', () => {
  assert.equal(trackIdFrom('k12'), 'k12');
  assert.equal(trackIdFrom('https://music.example.com/album/k3?track=k12'), 'k12');
  assert.equal(albumIdFrom('https://music.example.com/album/k3?track=k12'), 'k3');
  assert.equal(albumIdFrom('https://www.deezer.com/fr/album/302127'), '302127');
  assert.equal(artistIdFrom('https://music.example.com/artist/k7'), 'k7');
  assert.equal(artistIdFrom('https://music.example.com/artist/k7/discography/all'), 'k7');
  assert.equal(artistIdFrom('https://www.deezer.com/en/artist/27'), '27');
  assert.equal(artistIdFrom('k0'), null);
  assert.equal(artistIdFrom('https://music.example.com/artist/kx'), null);
  assert.equal(artistIdFrom('https://music.example.com/album/k3'), null);
  assert.ok(isLocalId('k1') && !isLocalId('123') && !isLocalId('k') && !isLocalId(null));
});
