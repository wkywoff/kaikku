import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cycleTarget } from '../src/lib/focusTrap.js';

const [a, b, c] = ['a', 'b', 'c'];

test('Tab from the last element goes back to the first, Shift+Tab from the first to the last', () => {
  assert.equal(cycleTarget([a, b, c], c, false), a);
  assert.equal(cycleTarget([a, b, c], a, true), c);
});

test('in the middle the browser moves focus itself', () => {
  assert.equal(cycleTarget([a, b, c], a, false), null);
  assert.equal(cycleTarget([a, b, c], b, true), null);
});

test('from the dialog itself (or anything outside the list) Tab enters at the edge', () => {
  assert.equal(cycleTarget([a, b, c], 'dialog', false), a);
  assert.equal(cycleTarget([a, b, c], 'dialog', true), c);
});

test('a single element keeps the focus', () => {
  assert.equal(cycleTarget([a], a, false), a);
  assert.equal(cycleTarget([a], a, true), a);
});

test('nothing to focus: no target', () => {
  assert.equal(cycleTarget([], 'dialog', false), null);
});
