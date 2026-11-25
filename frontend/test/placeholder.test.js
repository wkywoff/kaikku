import { test } from 'node:test';
import assert from 'node:assert/strict';
import { placeholderHue } from '../src/lib/placeholder.js';

test('placeholderHue matches the server', () => {
  assert.equal(placeholderHue('k123'), 200);
  assert.equal(placeholderHue('k7'), 36);
});
