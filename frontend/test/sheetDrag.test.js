import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decideDismiss, isBackSwipe, startsSideways } from '../src/lib/sheetDrag.js';

test('a sheet closes when dragged far down or flicked, and stays otherwise', () => {
  assert.equal(decideDismiss({ dy: 150, dt: 600, height: 400 }), true); // больше 30 % высоты
  assert.equal(decideDismiss({ dy: 60, dt: 80, height: 400 }), true); // резкий взмах вниз
  assert.equal(decideDismiss({ dy: 60, dt: 400, height: 400 }), false); // медленно и недалеко
  assert.equal(decideDismiss({ dy: 20, dt: 10, height: 400 }), false); // дрожь пальца
  assert.equal(decideDismiss({ dy: 0, dt: 5, height: 400 }), false); // вернули на место
});

test('a swipe right from the left edge means back, a vertical or short one does not', () => {
  assert.equal(isBackSwipe(60, 10), true);
  assert.equal(isBackSwipe(60, -80), false); // больше вверх, чем вправо
  assert.equal(isBackSwipe(25, 0), false); // слишком коротко
  assert.equal(isBackSwipe(-60, 0), false); // влево
});

test('the first movement decides whether an edge swipe can be back', () => {
  assert.equal(startsSideways(12, 3), true);
  assert.equal(startsSideways(3, -12), false); // сначала вверх
  assert.equal(startsSideways(2, 11), false); // сначала вниз
});
