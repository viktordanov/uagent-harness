import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tasksURL, renderRow, summarize } from './app.js';

test('tasksURL without a filter', () => {
  assert.equal(tasksURL(), '/api/tasks');
});

test('renderRow marks done tasks', () => {
  assert.equal(renderRow({ title: 'x', status: 'done' }), '✓ x');
  assert.equal(renderRow({ title: 'y', status: 'todo' }), '· y');
});

test('summarize counts statuses', () => {
  assert.deepEqual(summarize([{ status: 'todo' }, { status: 'done' }, { status: 'done' }]), { todo: 1, doing: 0, done: 2 });
});
