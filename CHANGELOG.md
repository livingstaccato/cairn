# Changelog

Format loosely follows [Keep a Changelog](https://keepachangelog.com/), and
versions follow [SemVer](https://semver.org/) once tagged. See the
`--version` flag for the version currently embedded in the default branch.

## main

### Added

- A `--version` flag on the root command. `go run`/`go build` report `dev`;
  `make build` (`ci/build.sh`) injects the tag `git describe` resolves via
  `-ldflags`, so the reported version tracks the tag rather than a source
  literal someone has to remember to bump.
- `cairndex serve --no-follow-symlinks` refuses any request path that
  traverses a symlink, rather than resolving it and checking where it lands.
- `pep503_level: root` or `pep503_level: project` turns the existing
  mixed-directory warning for `outputs: [pep503]` into a build error when a
  directory does not actually hold only projects or only files. Applies in
  both `mode: direct` and `mode: hugo`.
- `mode: hugo` now actually renders `outputs: [pep503]`. It previously did
  nothing there — `hugoRenders` treated pep503 the same as html, on the
  assumption Hugo's template would produce it, but no template ever did, so
  the directory silently got an ordinary listing instead. A new
  `layouts/partials/cairndex/pep503.html` renders it now, and `outputs:
  [html, pep503]` together is refused at config load in both modes, rather
  than only as a side effect of `mode: direct`'s write guard.
- `outputs: [search]` now writes a working search box at `search/`, beside
  `search-index.json` — dependency-free, no CDN, in both modes. Previously
  the JSON existed but nothing let a visitor actually search it short of
  wiring up Fuse.js or similar themselves.
- A JSON Schema for `cairndex.yaml` (`cairndex.schema.json`), and
  `cairndex init` now writes a `yaml-language-server` directive pointing at
  it, so a mistyped key shows up in an editor before the build ever runs.
- `cairndex init --mode hugo` writes the other starter: no `present:`/
  `outputs:` to pick, since Hugo renders the HTML.
- `cairndex build` now logs "build in progress" periodically on a build
  long enough for that to matter, instead of staying silent until the final
  "build complete" line whatever the tree's size.

### Changed

- `cairndex build` and `cairndex check` now stop promptly when interrupted
  mid-run, instead of only surviving a Ctrl-C to keep going. A build cut
  short still saves a manifest for what it wrote.
- Log output is colorized and human-formatted by default instead of a raw
  `time=... level=... filename=... lineno=...` slog line — meant for a
  service's own aggregator, not someone reading `cairndex build`'s output
  at a terminal. Plain (no ANSI) automatically when the output is not a
  terminal. `PROVIDE_LOG_FORMAT` and `PROVIDE_LOG_INCLUDE_CALLER` still
  override it, for scripting or debugging.

### Fixed

- The breadcrumb partial built its trail from a manual `base_path` string
  trim; a directory this disagreed with the site's real mount rendered
  broken links. It now walks Hugo's own page ancestors instead.
- `cairndex serve` opened and re-resolved a request path twice, widening the
  window a concurrent writer could swap a symlink into; it now opens the
  path containment already resolved.
- `layouts/partials/cairndex/entries.html` (search integration) has
  returned zero entries for every page since the listing moved from
  frontmatter into an `index.json` bundle resource — it read
  `.Params.cairndex.entries`, a field that move deleted, and nothing caught
  it because nothing rendered it. It now reads the resource, the same as
  every other presenter.
