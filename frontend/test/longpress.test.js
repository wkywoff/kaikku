import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { createLongPress } from '../src/lib/longpress.js';

test('a held finger fires after the delay, at the place it was pressed', () => {
  mock.timers.enable({ apis: ['setTimeout'] });
  try {
    const fired = [];
    const p = createLongPress((at) => fired.push(at));
    p.down(10, 20);
    mock.timers.tick(499);
    assert.equal(fired.length, 0);
    p.move(14, 22); // дрожь пальца — не прокрутка
    mock.timers.tick(1);
    assert.deepEqual(fired, [{ x: 10, y: 20 }]);
  } finally {
    mock.timers.reset();
  }
});

test('moving the finger (scrolling), lifting it or a native menu cancels', () => {
  mock.timers.enable({ apis: ['setTimeout'] });
  try {
    let fired = 0;
    const p = createLongPress(() => fired++);
    p.down(0, 0);
    p.move(0, 30);
    mock.timers.tick(1000);
    p.down(0, 0);
    mock.timers.tick(200);
    p.cancel();
    mock.timers.tick(1000);
    assert.equal(fired, 0);
  } finally {
    mock.timers.reset();
  }
});
