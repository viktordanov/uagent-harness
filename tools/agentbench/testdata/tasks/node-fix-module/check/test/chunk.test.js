import { test } from 'node:test';
import assert from 'node:assert/strict';
import { chunk } from '../src/index.js';

test('chunk', () => {
  assert.deepEqual(chunk([1, 2, 3, 4, 5], 2), [[1, 2], [3, 4], [5]]);
  assert.deepEqual(chunk([1, 2, 3, 4], 2), [[1, 2], [3, 4]]);
  assert.deepEqual(chunk([1], 3), [[1]]);
  assert.deepEqual(chunk([], 3), []);
  assert.throws(() => chunk([1], 0), RangeError);
});
