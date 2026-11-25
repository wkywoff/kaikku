import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decideSwipe } from '../src/lib/swipe.js';

test('far enough or a quick flick changes the song, a nudge does not', () => {
  assert.equal(decideSwipe({ dx: -150, dt: 600, width: 400 }), 'next');
  assert.equal(decideSwipe({ dx: 150, dt: 600, width: 400 }), 'prev');
  assert.equal(decideSwipe({ dx: -60, dt: 80, width: 400 }), 'next');
  assert.equal(decideSwipe({ dx: -60, dt: 400, width: 400 }), null);
  assert.equal(decideSwipe({ dx: -20, dt: 10, width: 400 }), null);
});
