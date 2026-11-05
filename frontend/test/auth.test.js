import { test } from 'node:test';
import assert from 'node:assert/strict';
import { get } from 'svelte/store';

const data = new Map();
global.localStorage = {
  getItem: (k) => data.get(k) ?? null,
  setItem: (k, v) => data.set(k, v),
  removeItem: (k) => data.delete(k),
};
let listener;
global.addEventListener = (type, fn) => {
  if (type === 'storage') listener = fn;
};
let reloads = 0;
global.location = { reload: () => reloads++ };

const { auth } = await import('../src/lib/stores/auth.js');
const token = (sub) => `h.${Buffer.from(JSON.stringify({ sub })).toString('base64url')}.s`;

test('renewing writes the fresh token', () => {
  auth.login(token('a'));
  auth.renew(token('a'), 'fresh');
  assert.equal(data.get('kaikku_token'), 'fresh');
  assert.equal(get(auth), 'fresh');
});

test('a stale tab does not bring back a token after logout elsewhere', () => {
  auth.login(token('a'));
  data.delete('kaikku_token'); // другая вкладка вышла, событие ещё не дошло
  auth.renew(token('a'), 'fresh');
  assert.equal(data.get('kaikku_token'), undefined);
});

test('a stale tab does not overwrite another account', () => {
  auth.login(token('a'));
  data.set('kaikku_token', token('b'));
  auth.renew(token('a'), 'fresh');
  assert.equal(data.get('kaikku_token'), token('b'));
});

test('logout in another tab reaches this one', () => {
  auth.login(token('a'));
  data.delete('kaikku_token');
  listener({ key: 'kaikku_token' });
  assert.equal(get(auth), null);
});

test('another account in another tab reloads this one, a renewal does not', () => {
  auth.login(token('a'));
  const before = reloads;
  data.set('kaikku_token', token('a') + 'x');
  listener({ key: 'kaikku_token' });
  assert.equal(reloads, before);
  data.set('kaikku_token', token('b'));
  listener({ key: 'kaikku_token' });
  assert.equal(reloads, before + 1);
  assert.equal(get(auth), token('b'));
});
