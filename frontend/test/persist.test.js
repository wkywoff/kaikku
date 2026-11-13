import { test } from 'node:test';
import assert from 'node:assert/strict';

const storage = new Map();
global.localStorage = {
  getItem: (k) => (storage.has(k) ? storage.get(k) : null),
  setItem: (k, v) => storage.set(k, String(v)),
  removeItem: (k) => storage.delete(k),
};

const { loadSavedQueue, saveQueue, saveQueuePosition } = await import('../src/lib/stores/player/persist.js');

const a = { id: 'a' };
const b = { id: 'b', source: { url: 'one-time' } };
const c = { id: 'c' };

test('the queue comes back with the position saved after it', () => {
  saveQueue({ queue: [a, b, c], index: 0, currentTime: 5 }, null);
  saveQueuePosition({ queue: [a, b, c], index: 2, currentTime: 41 });
  const q = loadSavedQueue();
  assert.deepEqual(q.queue, [{ id: 'a' }, { id: 'b' }, { id: 'c' }], 'одноразовая ссылка не сохраняется');
  assert.equal(q.index, 2);
  assert.equal(q.time, 41);
  assert.equal(q.order, null);
});

test('the unshuffled order is saved as places in the queue', () => {
  saveQueue({ queue: [c, a, b], index: 0, currentTime: 0 }, [a, b, c]);
  assert.deepEqual(loadSavedQueue().order, [1, 2, 0]);
});

test('a position saved for another queue is ignored', () => {
  saveQueue({ queue: [a, b, c], index: 2, currentTime: 30 }, null);
  // Другая вкладка или старая версия записала очередь заново, позицию не тронула.
  storage.set('kaikku_queue', JSON.stringify({ queue: [{ id: 'x' }, { id: 'y' }], index: 1, time: 7, order: null }));
  const q = loadSavedQueue();
  assert.deepEqual(q.queue.map((e) => e.id), ['x', 'y']);
  assert.equal(q.index, 1);
  assert.equal(q.time, 7);
});

test('a broken position falls back to the start of the queue, an empty queue forgets both', () => {
  saveQueue({ queue: [a, b], index: 0, currentTime: 0 }, null);
  saveQueuePosition({ index: 9, currentTime: -3 });
  assert.equal(loadSavedQueue().index, 0);
  assert.equal(loadSavedQueue().time, 0);

  saveQueue({ queue: [], index: -1, currentTime: 0 }, null);
  assert.equal(storage.has('kaikku_queue'), false);
  assert.equal(storage.has('kaikku_queue_pos'), false);
  assert.equal(loadSavedQueue(), null);
  saveQueuePosition({ index: 0, currentTime: 1 });
  assert.equal(storage.has('kaikku_queue_pos'), false, 'без очереди позицию не пишем');
});
