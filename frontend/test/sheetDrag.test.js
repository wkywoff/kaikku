import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decideDismiss } from '../src/lib/sheetDrag.js';

test('a sheet closes when dragged far down or flicked, and stays otherwise', () => {
  assert.equal(decideDismiss({ dy: 150, dt: 600, height: 400 }), true); // больше 30 % высоты
  assert.equal(decideDismiss({ dy: 60, dt: 80, height: 400 }), true); // резкий взмах вниз
  assert.equal(decideDismiss({ dy: 60, dt: 400, height: 400 }), false); // медленно и недалеко
  assert.equal(decideDismiss({ dy: 20, dt: 10, height: 400 }), false); // дрожь пальца
  assert.equal(decideDismiss({ dy: 0, dt: 5, height: 400 }), false); // вернули на место
});
