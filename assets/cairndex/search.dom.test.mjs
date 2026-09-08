// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Unit tests above cover scoreRecord/rankResults as pure functions. This
// file covers what those can't: init's own fetch-failure handling, and
// specifically the race between a failed index load and a visitor typing —
// see search-integration.md and the DOM-test comment in cairndex.dom.test.mjs
// for why this fixture is hand-built rather than imported from a template.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { init } from './search.js';

function fixture() {
  const dom = new JSDOM(`
    <div data-cairndex-search="../search-index.json">
      <input data-cairndex-search-input>
      <p data-cairndex-search-status></p>
      <ul data-cairndex-search-results></ul>
    </div>
  `, { runScripts: 'outside-only' });
  return dom.window.document;
}

function type(document, value) {
  const input = document.querySelector('[data-cairndex-search-input]');
  input.value = value;
  input.dispatchEvent(new document.defaultView.Event('input', { bubbles: true }));
}

const rejects = () => Promise.reject(new Error('network'));

test('a failed index still shows the failure message after typing', async () => {
  const document = fixture();
  init(document.querySelector('[data-cairndex-search]'), rejects);

  // Let the fetch/catch chain settle before anything is typed — the
  // ordinary case, not the race.
  await new Promise((r) => setTimeout(r, 0));

  type(document, 'nginx');

  assert.equal(
    document.querySelector('[data-cairndex-search-status]').textContent,
    'Search index failed to load.',
  );
});

// The race the bug lived in: a keystroke that lands before fetch's own
// .catch has run. The stale "0 results" from that keystroke has to be
// replaced once the failure is known, not left standing.
test('a keystroke before the fetch fails does not survive the failure', async () => {
  const document = fixture();
  let reject;
  const pending = new Promise((_, r) => {
    reject = r;
  });
  init(document.querySelector('[data-cairndex-search]'), () => pending);

  type(document, 'nginx');
  assert.equal(
    document.querySelector('[data-cairndex-search-status]').textContent,
    '0 results',
  );

  reject(new Error('network'));
  await new Promise((r) => setTimeout(r, 0));

  assert.equal(
    document.querySelector('[data-cairndex-search-status]').textContent,
    'Search index failed to load.',
  );
});

// A load that succeeds must never show the failure message — indexFailed
// has to stay false on the ordinary path, not just flip true correctly on
// the failing one. Queried for a miss rather than a hit: the rendered
// result <li> uses the page's own document, which this fixture's window
// does not stand in for, and a genuine miss (still a distinct string from
// the failure message) proves the point without needing it.
test('a working index is unaffected by the failure handling', async () => {
  const document = fixture();
  const records = [{ name: 'nginx', path: 'nginx/' }];
  init(document.querySelector('[data-cairndex-search]'), () =>
    Promise.resolve({ json: () => Promise.resolve(records) }),
  );

  await new Promise((r) => setTimeout(r, 0));

  type(document, 'no-such-package');
  assert.equal(
    document.querySelector('[data-cairndex-search-status]').textContent,
    '0 results',
  );
});
