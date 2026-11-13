import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  shuffled,
  followingIndex,
  startQueue,
  shuffleRest,
  restoreOrder,
  beforeAutoplay,
  afterUpNext,
  insertEntries,
  removeEntry,
  swapEntries,
  moveEntry,
  withoutAutoplayed,
} from '../src/lib/stores/player/queue.js';

const tracks = (...ids) => ids.map((id) => ({ id }));
const ids = (list) => list.map((e) => e.id).join('');

test('shuffled keeps every item and leaves the input alone', () => {
  const items = tracks('a', 'b', 'c', 'd');
  const out = shuffled(items, () => 0);
  assert.equal(ids(items), 'abcd');
  assert.equal(ids(out), 'bcda');
});

test('followingIndex: next track, wrap on repeat, null at the end', () => {
  const queue = tracks('a', 'b');
  assert.equal(followingIndex({ queue, index: 0, repeat: 'off' }), 1);
  assert.equal(followingIndex({ queue, index: 1, repeat: 'off' }), null);
  assert.equal(followingIndex({ queue, index: 1, repeat: 'all' }), 0);
  assert.equal(followingIndex({ queue: [], index: -1, repeat: 'all' }), null);
});

test('startQueue: as given without shuffle, chosen track first with it', () => {
  const list = tracks('a', 'b', 'c');
  assert.deepEqual(startQueue(list, 1, false), { queue: list, index: 1, unshuffled: null });
  assert.equal(startQueue(list, null, false).index, 0);

  const q = startQueue(list, 2, true);
  assert.equal(q.queue[0].id, 'c');
  assert.equal(q.index, 0);
  assert.equal(q.unshuffled, list);
  assert.equal(ids([...q.queue].sort((x, y) => x.id.localeCompare(y.id))), 'abc');
});

test('shuffleRest keeps the current track playing, restoreOrder puts it back', () => {
  const list = tracks('a', 'b', 'c', 'd');
  const on = shuffleRest({ queue: list, index: 2 });
  assert.equal(on.queue[0], list[2]);
  assert.equal(on.index, 0);
  assert.equal(on.unshuffled, list);

  const off = restoreOrder(on);
  assert.deepEqual(off, { queue: list, index: 2, unshuffled: null });
});

test('added to the queue goes before autoplay, in both orders', () => {
  const [a, b, c] = tracks('a', 'b', 'c');
  const auto = { id: 'x', autoplay: true };
  const q = insertEntries({ queue: [a, auto, b], index: 0, unshuffled: [a, b, auto] }, [c], beforeAutoplay);
  assert.equal(ids(q.queue), 'acxb');
  assert.equal(q.at, 1);
  assert.equal(ids(q.unshuffled), 'abcx');
});

test('"play next" goes after what was already put next', () => {
  const [a, b, c] = tracks('a', 'b', 'c');
  const first = { id: 'n', upNext: true };
  const q = insertEntries({ queue: [a, first, b], index: 0, unshuffled: null }, [c], afterUpNext);
  assert.equal(ids(q.queue), 'ancb');
  assert.equal(q.at, 2);
  assert.equal(q.unshuffled, null);
});

test('removeEntry keeps the current track current', () => {
  const list = tracks('a', 'b', 'c');
  const q = (index) => ({ queue: list, index, unshuffled: [list[2], list[1], list[0]] });
  assert.equal(removeEntry(q(2), 0).index, 1); // убрали до текущего
  assert.equal(removeEntry(q(0), 2).index, 0); // после
  assert.equal(removeEntry(q(1), 1).index, 1); // сам текущий — играет следующий
  assert.equal(removeEntry(q(2), 2).index, 1); // текущий был последним
  assert.equal(ids(removeEntry(q(0), 1).unshuffled), 'ca');
  assert.deepEqual(removeEntry({ queue: [list[0]], index: 0, unshuffled: null }, 0), { queue: [], index: -1, unshuffled: null });
});

test('swapEntries and moveEntry follow the current track', () => {
  const list = tracks('a', 'b', 'c', 'd');
  const swapped = swapEntries({ queue: list, index: 1 }, 1, 2);
  assert.equal(ids(swapped.queue), 'acbd');
  assert.equal(swapped.index, 2);
  assert.equal(swapEntries({ queue: list, index: 0 }, 1, 2).index, 0);

  const moved = moveEntry({ queue: list, index: 1 }, 3, 0);
  assert.equal(ids(moved.queue), 'dabc');
  assert.equal(moved.index, 2);
});

test('withoutAutoplayed drops only what autoplay added after the current track', () => {
  const [a, b] = tracks('a', 'b');
  const played = { id: 'p', autoplay: true };
  const ahead = { id: 'x', autoplay: true };
  const q = withoutAutoplayed({ queue: [a, played, b, ahead], index: 1, unshuffled: [a, b, played, ahead] });
  assert.equal(ids(q.queue), 'apb');
  assert.equal(ids(q.unshuffled), 'abp');
});
