import { test } from 'node:test';
import assert from 'node:assert/strict';
import { get } from 'svelte/store';

const data = new Map();
global.localStorage = {
  getItem: (k) => data.get(k) ?? null,
  setItem: (k, v) => data.set(k, v),
  removeItem: (k) => data.delete(k),
};

// Запрос через fetch: запоминаем адрес, отвечаем пустым JSON.
let fetched = [];
global.fetch = async (url) => {
  fetched.push(url);
  return { ok: true, status: 200, headers: { get: () => null }, json: async () => ({}) };
};

// Загрузка файла идёт через XHR: ответ задаёт тест.
let reply = { status: 200, body: '{}', headers: {} };
let sent = [];
global.XMLHttpRequest = class {
  upload = {};
  open(method, url) {
    sent.push({ method, url });
  }
  setRequestHeader() {}
  getResponseHeader(name) {
    return reply.headers[name] ?? null;
  }
  send() {
    this.status = reply.status;
    this.responseText = reply.body;
    queueMicrotask(() => this.onload());
  }
};

const { auth } = await import('../src/lib/stores/auth.js');
const { api } = await import('../src/lib/api.js');
const file = { type: 'audio/flac', name: 'a.flac' };

test('a successful upload keeps the renewed token', async () => {
  auth.login('old');
  reply = { status: 200, body: '{"track":{"id":"k1"}}', headers: { 'X-Auth-Token': 'fresh' } };
  const res = await api.replaceTrackAudio('k1', file);
  assert.equal(res.track.id, 'k1');
  assert.equal(get(auth), 'fresh');
  assert.equal(data.get('kaikku_token'), 'fresh');
});

test('a failed upload keeps the renewed token too and reports the error', async () => {
  auth.login('old');
  reply = { status: 413, body: '<html>too large</html>', headers: { 'X-Auth-Token': 'fresh' } };
  await assert.rejects(api.replaceTrackAudio('k1', file), (err) => err.status === 413);
  assert.equal(get(auth), 'fresh');
});

test('an upload answered with 401 logs out', async () => {
  auth.login('old');
  reply = { status: 401, body: '{"error":"invalid or expired token"}', headers: {} };
  await assert.rejects(api.replaceTrackAudio('k1', file), (err) => err.status === 401);
  assert.equal(get(auth), null);
});

test('ids are encoded in every path', async () => {
  fetched = [];
  sent = [];
  await api.getPlaylist('a/b');
  await api.like('1?x=2');
  await api.removeFromPlaylist('p 1', 'i#2');
  await api.ingestLookup(['1', '2&3']);
  await api.artistBio('27', 'ru');
  reply = { status: 200, body: '{}', headers: {} };
  await api.uploadTrackAudio('5/6', file);
  assert.deepEqual(fetched, [
    '/api/playlists/a%2Fb',
    '/api/me/liked/1%3Fx%3D2',
    '/api/playlists/p%201/tracks/i%232',
    '/api/ingest/lookup?ids=1,2%263',
    '/api/discover/artists/27/bio?lang=ru',
  ]);
  assert.deepEqual(sent, [{ method: 'PUT', url: '/api/tracks/deezer/5%2F6/audio' }]);
});
