import { test } from 'node:test';
import assert from 'node:assert/strict';
import { get } from 'svelte/store';

const data = new Map();
global.localStorage = {
  getItem: (k) => data.get(k) ?? null,
  setItem: (k, v) => data.set(k, v),
  removeItem: (k) => data.delete(k),
};

// Загрузка файла идёт через XHR: ответ задаёт тест.
let reply = { status: 200, body: '{}', headers: {} };
global.XMLHttpRequest = class {
  upload = {};
  open() {}
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
