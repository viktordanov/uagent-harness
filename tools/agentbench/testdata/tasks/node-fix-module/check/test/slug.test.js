import { test } from 'node:test';
import assert from 'node:assert/strict';
import { slugify } from '../src/index.js';

test('slugify', () => {
  assert.equal(slugify('Hello World'), 'hello-world');
  assert.equal(slugify('  Crème Brûlée -- Recipe! '), 'creme-brulee-recipe');
  assert.equal(slugify('Go 1.22 released'), 'go-1-22-released');
  assert.equal(slugify('---'), '');
});
