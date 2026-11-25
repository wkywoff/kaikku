import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decideDismiss, isBackSwipe, startsSideways } from '../src/lib/sheetDrag.js';

test('a sheet closes when dragged far down or flicked, and stays otherwise', () => {
  assert.equal(decideDismiss({ dy: 150, dt: 600, height: 400 }), true);
  assert.equal(decideDismiss({ dy: 60, dt: 80, height: 400 }), true);
  assert.equal(decideDismiss({ dy: 60, dt: 400, height: 400 }), false);
  assert.equal(decideDismiss({ dy: 20, dt: 10, height: 400 }), false);
  assert.equal(decideDismiss({ dy: 0, dt: 5, height: 400 }), false);
});

test('a swipe right from the left edge means back, a vertical or short one does not', () => {
  assert.equal(isBackSwipe(60, 10), true);
  assert.equal(isBackSwipe(60, -80), false);
  assert.equal(isBackSwipe(25, 0), false);
  assert.equal(isBackSwipe(-60, 0), false);
});

test('the first movement decides whether an edge swipe can be back', () => {
  assert.equal(startsSideways(12, 3), true);
  assert.equal(startsSideways(3, -12), false);
  assert.equal(startsSideways(2, 11), false);
});
