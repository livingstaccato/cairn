// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sortRows, filterRows } from './cairn.js';

function row(name, size, modified) {
  return { dataset: { name, size: String(size), modified }, hidden: false };
}

test('sortRows by name ascending', () => {
  const rows = [row('zeta', 1, '2026-01-01'), row('alpha', 2, '2026-01-02')];
  assert.deepEqual(sortRows(rows, 'name', 'asc').map((r) => r.dataset.name), ['alpha', 'zeta']);
});

test('sortRows by size is numeric, not lexicographic', () => {
  const rows = [row('a', 9, 'x'), row('b', 100, 'x'), row('c', 20, 'x')];
  const got = sortRows(rows, 'size', 'asc').map((r) => Number(r.dataset.size));
  assert.deepEqual(got, [9, 20, 100]);
});

test('sortRows descending reverses', () => {
  const rows = [row('a', 1, 'x'), row('b', 2, 'x')];
  assert.deepEqual(sortRows(rows, 'size', 'desc').map((r) => r.dataset.name), ['b', 'a']);
});

test('sortRows by modified', () => {
  const rows = [row('a', 1, '2026-05-01'), row('b', 2, '2026-01-01')];
  assert.deepEqual(sortRows(rows, 'modified', 'asc').map((r) => r.dataset.name), ['b', 'a']);
});

test('sortRows does not mutate its input', () => {
  const rows = [row('zeta', 1, 'x'), row('alpha', 2, 'x')];
  sortRows(rows, 'name', 'asc');
  assert.equal(rows[0].dataset.name, 'zeta');
});

test('filterRows is case-insensitive substring on name', () => {
  const rows = [row('Ubuntu.iso', 1, 'x'), row('apt.list', 2, 'x')];
  filterRows(rows, 'UBU');
  assert.equal(rows[0].hidden, false);
  assert.equal(rows[1].hidden, true);
});

test('empty filter shows everything', () => {
  const rows = [row('a', 1, 'x'), row('b', 2, 'x')];
  filterRows(rows, 'zzz');
  filterRows(rows, '');
  assert.ok(rows.every((r) => r.hidden === false));
});
