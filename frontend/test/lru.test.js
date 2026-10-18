import { test } from 'node:test';
import assert from 'node:assert/strict';
import { LRU } from '../src/lib/lru.js';

test('LRU keeps at most max entries, dropping the least recently used', () => {
  const c = new LRU(3);
  c.set('a', 1).set('b', 2).set('c', 3);
  assert.equal(c.get('a'), 1); // a — снова свежий
  c.set('d', 4); // вытесняет b, самый давний
  assert.deepEqual([...c.keys()], ['c', 'a', 'd']);
  assert.equal(c.has('b'), false);
  assert.equal(c.get('b'), undefined);
  c.set('c', 30); // перезапись тоже делает свежим
  c.set('e', 5);
  assert.deepEqual([...c.keys()], ['d', 'c', 'e']);
  assert.equal(c.size, 3);
  c.delete('c');
  assert.deepEqual([...c.keys()], ['d', 'e']);
});
