import { test } from 'node:test';
import assert from 'node:assert/strict';
import { countLiked } from '../src/lib/artistHighlights.js';

test('liked songs are counted once, and only while they are liked', () => {
  const liked = new Set(['1', '2', '3']);
  assert.equal(countLiked(liked, ['1', '2'], ['2', '3', '4']), 3);
  assert.equal(countLiked(liked, ['9'], []), 0);
  assert.equal(countLiked(new Set(), ['1'], ['2']), 0);
});
