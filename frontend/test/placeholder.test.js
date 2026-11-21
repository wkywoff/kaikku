import { test } from 'node:test';
import assert from 'node:assert/strict';
import { placeholderHue } from '../src/lib/placeholder.js';

// Те же значения, что в internal/api/placeholder_test.go: превью совпадает с заглушкой сервера.
test('placeholderHue matches the server', () => {
  assert.equal(placeholderHue('k123'), 200);
  assert.equal(placeholderHue('k7'), 36);
});
