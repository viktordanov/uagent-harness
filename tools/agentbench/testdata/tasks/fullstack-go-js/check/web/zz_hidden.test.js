import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tasksURL, statusOptions } from './app.js';

test('hidden: tasksURL filters', () => {
  assert.equal(tasksURL(), '/api/tasks');
  assert.equal(tasksURL(''), '/api/tasks');
  assert.equal(tasksURL('done'), '/api/tasks?status=done');
  assert.equal(tasksURL('doing'), '/api/tasks?status=doing');
});

test('hidden: statusOptions', () => {
  const opts = statusOptions();
  assert.deepEqual(opts.map((o) => o.value), ['', 'todo', 'doing', 'done']);
  assert.equal(opts[0].label, 'All');
  for (const o of opts) assert.equal(typeof o.label, 'string');
  for (const o of opts) assert.ok(o.label.length > 0);
});
