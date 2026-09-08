// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// A dependency-free full-text search over search-index.json: no CDN, no
// bundler, works airgapped — the same reason the styled listing's own
// sort/filter carries no library either. scoreRecord and rankResults hold no
// DOM and are exported for testing; the wiring below is the only part that
// touches the page, mirroring cairndex.js's own split.

const NAME_WEIGHT = 3;
const TITLE_WEIGHT = 3;
const SUMMARY_WEIGHT = 1;
const TAGS_WEIGHT = 1;
const MAX_RESULTS = 200;

// scoreRecord ranks name and title above summary and tags: on a file mirror
// people search for filenames — "nginx_1.24.0-1_amd64.deb" — far more often
// than for prose, and a title is frequently absent. 0 means no match.
export function scoreRecord(record, needle) {
  if (!needle) return 0;
  const fields = [
    [record.name, NAME_WEIGHT],
    [record.title, TITLE_WEIGHT],
    [record.summary, SUMMARY_WEIGHT],
    [(record.tags || []).join(' '), TAGS_WEIGHT],
  ];
  let score = 0;
  for (const [text, weight] of fields) {
    if (text && text.toLowerCase().includes(needle)) score += weight;
  }
  return score;
}

// rankResults is what the input handler calls: an empty query returns no
// results rather than the whole index, since rendering every entry on an
// untouched search box reads as broken, not helpful.
export function rankResults(records, query, limit = MAX_RESULTS) {
  const needle = query.trim().toLowerCase();
  if (!needle) return [];
  return records
    .map((r) => [scoreRecord(r, needle), r])
    .filter(([score]) => score > 0)
    .sort((a, b) => b[0] - a[0])
    .slice(0, limit)
    .map(([, r]) => r);
}

if (typeof document !== 'undefined') {
  document.addEventListener('DOMContentLoaded', () => {
    const root = document.querySelector('[data-cairndex-search]');
    if (!root) return;
    const input = root.querySelector('[data-cairndex-search-input]');
    const status = root.querySelector('[data-cairndex-search-status]');
    const list = root.querySelector('[data-cairndex-search-results]');
    let records = [];

    fetch(root.dataset.cairndexSearch)
      .then((r) => r.json())
      .then((data) => {
        records = data;
      })
      .catch(() => {
        status.textContent = 'Search index failed to load.';
      });

    input.addEventListener('input', () => {
      list.textContent = '';
      if (!input.value.trim()) {
        status.textContent = '';
        return;
      }
      const results = rankResults(records, input.value);
      status.textContent = `${results.length} result${results.length === 1 ? '' : 's'}`;
      for (const r of results) {
        const li = document.createElement('li');
        const a = document.createElement('a');
        a.href = r.path;
        a.textContent = r.title || r.name;
        li.appendChild(a);
        if (r.summary) {
          const small = document.createElement('small');
          small.textContent = ' — ' + r.summary;
          li.appendChild(small);
        }
        list.appendChild(li);
      }
    });
  });
}
