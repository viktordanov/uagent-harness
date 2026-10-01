import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tasksURL, statusOptions } from './app.js';

test('tasksURL adds the status', () => {
  assert.equal(tasksURL('done'), '/api/tasks?status=done');
});

test('statusOptions starts with All', () => {
  assert.equal(statusOptions()[0].value, '');
});
