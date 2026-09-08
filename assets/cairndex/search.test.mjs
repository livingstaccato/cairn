// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { scoreRecord, rankResults } from './search.js';

test('scoreRecord matches on name case-insensitively', () => {
  assert.ok(scoreRecord({ name: 'nginx_1.24.0-1_amd64.deb' }, 'nginx') > 0);
});

// scoreRecord is exported for reuse outside the DOM wiring (a host folding
// this into its own search index, per search-integration.md), so it cannot
// assume a caller already lowercased the needle the way rankResults does —
// it has to be case-insensitive on both sides, on its own.
test('scoreRecord is case-insensitive on the needle, not just the haystack', () => {
  assert.ok(scoreRecord({ name: 'nginx' }, 'NGINX') > 0);
  assert.ok(scoreRecord({ name: 'NGINX' }, 'nginx') > 0);
});

test('scoreRecord weighs name and title above summary and tags', () => {
  const nameHit = scoreRecord({ name: 'nginx' }, 'nginx');
  const summaryHit = scoreRecord({ name: 'x', summary: 'nginx docs' }, 'nginx');
  assert.ok(nameHit > summaryHit);
});

test('scoreRecord searches tags', () => {
  assert.ok(scoreRecord({ name: 'x', tags: ['web-server', 'nginx'] }, 'nginx') > 0);
});

test('scoreRecord returns 0 for no match', () => {
  assert.equal(scoreRecord({ name: 'apache' }, 'nginx'), 0);
});

test('scoreRecord returns 0 for an empty needle', () => {
  assert.equal(scoreRecord({ name: 'apache' }, ''), 0);
});

test('scoreRecord tolerates missing optional fields', () => {
  assert.doesNotThrow(() => scoreRecord({ name: 'x' }, 'x'));
});

test('rankResults returns nothing for an empty or whitespace query', () => {
  const records = [{ name: 'nginx' }];
  assert.deepEqual(rankResults(records, ''), []);
  assert.deepEqual(rankResults(records, '   '), []);
});

test('rankResults orders by score, best match first', () => {
  const records = [
    { name: 'x', summary: 'nginx docs' },
    { name: 'nginx-common' },
  ];
  const got = rankResults(records, 'nginx').map((r) => r.name);
  assert.deepEqual(got, ['nginx-common', 'x']);
});

test('rankResults excludes non-matching records', () => {
  const records = [{ name: 'nginx' }, { name: 'apache' }];
  assert.deepEqual(rankResults(records, 'nginx').map((r) => r.name), ['nginx']);
});

test('rankResults respects the limit', () => {
  const records = Array.from({ length: 10 }, (_, i) => ({ name: `nginx-${i}` }));
  assert.equal(rankResults(records, 'nginx', 3).length, 3);
});
