// SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
// SPDX-License-Identifier: MIT

// Unit tests above cover sortRows/filterRows as pure functions. This file
// covers what those can't: does init() wire the DOM up correctly — the click
// target, which element aria-sort actually lands on, whether the filter
// status announces the right count. It builds a fixture shaped like
// listing-styled.html's real output rather than importing that template,
// since Hugo templates aren't renderable outside Hugo; a shape drift between
// the two would only be caught by ci/example.sh's grep assertions or a
// browser, not by this file.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
import { init } from './cairndex.js';

function fixture() {
  const dom = new JSDOM(`
    <div class="cairndex" data-cairndex-present="styled">
      <div class="cairndex-toolbar" data-cairndex-toolbar hidden>
        <input data-cairndex-filter>
        <p data-cairndex-filter-status></p>
      </div>
      <div class="cairndex-listing-head" role="row">
        <span class="col-name" role="columnheader" aria-sort="ascending"><button data-sort="name">Name</button></span>
        <span class="col-meta" role="columnheader"><button data-sort="size">Size</button></span>
      </div>
      <div data-cairndex-body>
        <div class="cairndex-row" role="row" data-name="zeta" data-size="100" data-modified="2026-01-01">
          <a role="cell">zeta</a>
        </div>
        <div class="cairndex-row" role="row" data-name="alpha" data-size="5" data-modified="2026-01-02">
          <a role="cell">alpha</a>
        </div>
      </div>
    </div>
  `);
  return dom.window.document;
}

test('clicking a sort button moves aria-sort to its columnheader, not the button', () => {
  const document = fixture();
  const root = document.querySelector('.cairndex');
  init(root);

  const sizeBtn = document.querySelector('[data-sort="size"]');
  sizeBtn.dispatchEvent(new document.defaultView.Event('click', { bubbles: true }));

  assert.equal(sizeBtn.closest('[role="columnheader"]').getAttribute('aria-sort'), 'ascending');
  assert.equal(sizeBtn.getAttribute('aria-sort'), null);
  assert.equal(document.querySelector('.col-name').getAttribute('aria-sort'), null);
});

test('clicking a sort button reorders the rows in the DOM', () => {
  const document = fixture();
  const root = document.querySelector('.cairndex');
  init(root);

  document.querySelector('[data-sort="size"]').dispatchEvent(new document.defaultView.Event('click', { bubbles: true }));

  const names = [...document.querySelectorAll('.cairndex-row')].map((r) => r.dataset.name);
  assert.deepEqual(names, ['alpha', 'zeta']); // size ascending: 5 before 100
});

test('a second click on the same header reverses the order', () => {
  const document = fixture();
  const root = document.querySelector('.cairndex');
  init(root);

  const btn = document.querySelector('[data-sort="size"]');
  btn.dispatchEvent(new document.defaultView.Event('click', { bubbles: true }));
  btn.dispatchEvent(new document.defaultView.Event('click', { bubbles: true }));

  assert.equal(btn.closest('[role="columnheader"]').getAttribute('aria-sort'), 'descending');
  const names = [...document.querySelectorAll('.cairndex-row')].map((r) => r.dataset.name);
  assert.deepEqual(names, ['zeta', 'alpha']);
});

test('filtering hides non-matching rows and announces the count', () => {
  const document = fixture();
  const root = document.querySelector('.cairndex');
  init(root);

  const filter = document.querySelector('[data-cairndex-filter]');
  filter.value = 'alp';
  filter.dispatchEvent(new document.defaultView.Event('input', { bubbles: true }));

  const rows = [...document.querySelectorAll('.cairndex-row')];
  assert.equal(rows.find((r) => r.dataset.name === 'alpha').hidden, false);
  assert.equal(rows.find((r) => r.dataset.name === 'zeta').hidden, true);
  assert.equal(document.querySelector('[data-cairndex-filter-status]').textContent, 'Showing 1 of 2');
});

test('clearing the filter shows everything again and updates the count', () => {
  const document = fixture();
  const root = document.querySelector('.cairndex');
  init(root);

  const filter = document.querySelector('[data-cairndex-filter]');
  filter.value = 'alp';
  filter.dispatchEvent(new document.defaultView.Event('input', { bubbles: true }));
  filter.value = '';
  filter.dispatchEvent(new document.defaultView.Event('input', { bubbles: true }));

  assert.equal(document.querySelector('[data-cairndex-filter-status]').textContent, 'Showing 2 of 2');
});

test('the toolbar is revealed once JavaScript runs', () => {
  const document = fixture();
  const root = document.querySelector('.cairndex');
  assert.equal(root.querySelector('[data-cairndex-toolbar]').hidden, true);
  init(root);
  assert.equal(root.querySelector('[data-cairndex-toolbar]').hidden, false);
});
