// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: Apache-2.0

/**
 * cairn listing enhancement.
 *
 * Progressive enhancement only: the full listing is already in the HTML before
 * this runs. The toolbar ships hidden and is revealed here, so a visitor with
 * JavaScript disabled never sees a control that does nothing. The `bare`
 * presenter never loads this file at all.
 *
 * The exported functions are pure so they can be tested without a DOM.
 */

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/** Returns a new array of rows sorted by key ('name' | 'size' | 'modified'). */
export function sortRows(rows, key, order) {
  const sorted = [...rows].sort((a, b) => {
    if (key === 'size') {
      return Number(a.dataset.size) - Number(b.dataset.size);
    }
    if (key === 'modified') {
      return String(a.dataset.modified).localeCompare(String(b.dataset.modified));
    }
    return collator.compare(a.dataset.name, b.dataset.name);
  });
  return order === 'desc' ? sorted.reverse() : sorted;
}

/** Hides rows whose name does not contain the query, case-insensitively. */
export function filterRows(rows, query) {
  const q = query.trim().toLowerCase();
  for (const r of rows) {
    r.hidden = q !== '' && !r.dataset.name.toLowerCase().includes(q);
  }
}

/** Hides rows deeper than maxDepth. Only meaningful on a recursive listing. */
export function applyDepth(rows, maxDepth) {
  for (const r of rows) {
    r.hidden = Number(r.dataset.depth) > maxDepth;
  }
}

function init(root) {
  const body = root.querySelector('[data-cairn-body]');
  if (!body) return;
  const rows = () => Array.from(body.querySelectorAll('.cairn-row'));

  const toolbar = root.querySelector('[data-cairn-toolbar]');
  if (toolbar) toolbar.hidden = false;

  const filter = root.querySelector('[data-cairn-filter]');
  if (filter) {
    filter.addEventListener('input', () => filterRows(rows(), filter.value));
  }

  for (const th of root.querySelectorAll('th[data-sort]')) {
    th.addEventListener('click', () => {
      const key = th.dataset.sort;
      const order = th.getAttribute('aria-sort') === 'ascending' ? 'desc' : 'asc';
      for (const other of root.querySelectorAll('th[data-sort]')) {
        other.removeAttribute('aria-sort');
      }
      th.setAttribute('aria-sort', order === 'asc' ? 'ascending' : 'descending');
      for (const r of sortRows(rows(), key, order)) {
        body.appendChild(r);
      }
    });
  }
}

if (typeof document !== 'undefined') {
  document.addEventListener('DOMContentLoaded', () => {
    for (const root of document.querySelectorAll('.cairn[data-cairn-present="styled"]')) {
      init(root);
    }
  });
}
