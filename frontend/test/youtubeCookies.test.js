import { test } from 'node:test';
import assert from 'node:assert/strict';
import { messages } from '../src/lib/i18n.js';
import { cookieUploadMessage, signInHint } from '../src/lib/youtubeCookies.js';

test('the toast after an upload names what still stands in the way first', () => {
  const key = (res) => cookieUploadMessage(res).key;
  assert.equal(key({ check: 'blocked', signin: '', requeued: 0 }), 'missing.cookiesCheck.blocked');
  assert.equal(key({ check: 'ok', signin: 'age', requeued: 2 }), 'missing.cookiesSignIn.age');
  assert.equal(key({ check: 'ok', signin: 'refused', requeued: 0 }), 'missing.cookiesSignIn.refused');
  assert.deepEqual(cookieUploadMessage({ check: 'ok', signin: 'ok', requeued: 3 }), {
    key: 'missing.cookiesCheck.requeued',
    params: { n: 3 },
    kind: 'info',
  });
  assert.equal(key({ check: 'error', signin: '', requeued: 0 }), 'missing.cookiesCheck.error');
  assert.equal(key({ check: 'ok', signin: '', requeued: 0 }), 'missing.cookiesCheck.ok');
  assert.equal(key({ check: 'skipped', signin: '', requeued: 0 }), 'missing.cookiesCheck.skipped');
  assert.equal(cookieUploadMessage({ check: 'blocked' }).kind, 'error');
});

test('a song that needs a sign-in gets a hint for its case', () => {
  const adult = "signin: download: … Sorry, this content is age-restricted. To view this video, please provide more info so we can be sure you're an adult.";
  assert.equal(signInHint(adult, true), 'missing.signinAge');
  assert.equal(signInHint(adult, false), 'missing.signinNoCookies');
  assert.equal(signInHint('signin: download: … Sign in to confirm your age', false), 'missing.signinNoCookies');
  assert.equal(signInHint('signin: download: … Sign in to confirm your age', true), 'missing.signinRetry');
  assert.equal(signInHint(undefined, false), 'missing.signinNoCookies');
});

test('every key the cookies card can show exists in both languages', () => {
  const keys = [
    'missing.signin',
    'missing.signinAge',
    'missing.signinRetry',
    'missing.signinNoCookies',
    'missing.cookiesSignIn.age',
    'missing.cookiesSignIn.refused',
    ...['blocked', 'requeued', 'error', 'ok', 'skipped'].map((c) => `missing.cookiesCheck.${c}`),
  ];
  for (const lang of ['ru', 'en']) for (const k of keys) assert.ok(messages[lang][k], `${lang} ${k}`);
});
