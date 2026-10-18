import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tagTitle, mixTitle, mixSubtitle, mixHue, mixColor } from '../src/lib/mixes.js';

const t = (key, p = {}) => `${key}${Object.keys(p).length ? JSON.stringify(p) : ''}`;

test('tagTitle', () => {
  assert.equal(tagTitle('russian hip-hop'), 'Russian Hip-Hop');
  assert.equal(tagTitle('r&b'), 'R&B');
  assert.equal(tagTitle('edm'), 'EDM');
  assert.equal(tagTitle('80s'), '80s');
});

test('mixTitle and subtitle', () => {
  assert.equal(mixTitle({ kind: 'on_repeat' }, t), 'mix.onRepeat');
  assert.equal(mixTitle({ kind: 'tag', tag: 'phonk' }, t), 'mix.tag{"tag":"Phonk"}');
  assert.equal(mixSubtitle({ artists: ['A', 'B'] }, t), 'mix.artists{"names":"A, B"}');
  assert.equal(mixSubtitle({ artists: [] }, t), 'mix.kind');
});

test('mix colour is stable and valid', () => {
  assert.equal(mixHue('tag-phonk'), mixHue('tag-phonk'));
  assert.notEqual(mixHue('tag-phonk'), mixHue('tag-rap'));
  const c = mixColor('tag-phonk');
  for (const v of [c.r, c.g, c.b]) assert.ok(v >= 0 && v <= 255);
  assert.deepEqual(mixColor('on-repeat'), mixColor('on-repeat'));
});

test('tagSlug matches the server', async () => {
  const { tagSlug } = await import('../src/lib/mixes.js');
  assert.equal(tagSlug('russian hip-hop'), 'russian-hip-hop');
  assert.equal(tagSlug('r&b'), 'r-b');
  assert.equal(tagSlug('  lo-fi '), 'lo-fi');
  assert.equal(tagSlug('80s'), '80s');
});
