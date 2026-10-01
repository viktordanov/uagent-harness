import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatCents } from '../src/index.js';

test('formatCents', () => {
  assert.equal(formatCents(0), '$0.00');
  assert.equal(formatCents(5), '$0.05');
  assert.equal(formatCents(123456), '$1,234.56');
  assert.equal(formatCents(100000000), '$1,000,000.00');
  assert.equal(formatCents(-5), '-$0.05');
  assert.equal(formatCents(-123456), '-$1,234.56');
});
