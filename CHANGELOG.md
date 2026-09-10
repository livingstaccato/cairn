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
- `cairndex serve` answers a miss or a rejected method with a small styled
  HTML page instead of the standard library's plain text, and
  `--verbose-errors` names the specific reason instead of the generic
  "not found" every miss gets by default.
- The breadcrumb has a real identity now — it previously had none at all,
  rendering as the browser's own default blue underlined links with no
  spacing around the separators. The root crumb is a home icon, widened to
  meet WCAG 2.2's 24px minimum target size, underlined — a border under
  the icon, since `text-decoration` never draws under a replaced element —
  when it is also the current location; the directory icon across the
  whole sprite is a small stack of stones now, the mark the product is
  named after, not a generic folder outline. Only the current location is
  ever underlined, never the separators between crumbs, which are
  lightened further than a flat color computation alone suggested.
- Every listing page carries a small footer banner by default: when the
  build ran, and the `--version` cairndex reports. `build_info: false`
  turns it off.
- `outputs: [search]`'s search page has real styling now — it shipped with
  none at all: a default serif heading, plain black-on-white, no relation
  to the rest of the theme, and no way back to the directory it searches.
- `show_owner: true` adds owner, group and permission columns to every
  format that carries them (CSV, and both HTML presenters). Off by
  default: a real uid or username on a public mirror is the same class of
  leak this project already treats seriously for filenames. Windows has no
  uid/gid equivalent and only ever gets the permission column.
- A `server` Docker build target (`docker build --target server`): an
  image with nothing in it but the `cairndex` binary, running `watch
  --serve` against a mounted volume — a documented, verified
  single-container deployment (see `docs/deployment/container.md`), not
  just an implicit possibility nobody had actually run.
- `outputs: [atom]` writes `atom.xml` beside the normal listing: the
  directory's most recently modified entries, newest first, capped at 100.
  index.json/csv/txt already carry a listing in full; a feed's job is
  different — what changed recently — so it is capped and sorted rather
  than a fourth complete copy of the same data.
- `cairndex serve --metrics` and `cairndex watch --serve --metrics` reserve
  `/healthz` and `/metrics` on the server: JSON liveness/readiness, and a
  Prometheus text-exposition scrape target covering request counts and,
  under `watch --serve`, the build loop's own success/failure/timing. Off
  by default, so a served tree keeping a real `metrics/` directory is still
  served faithfully unless an operator explicitly asks to trade those two
  names away. See `docs/deployment/container.md`'s "Health checks and metrics".
- `CAIRNDEX_LOG_FORMAT` picks `pretty` or `json` in cairndex's own
  vocabulary, the same precedence `CAIRNDEX_ENVIRONMENT` already had over
  the telemetry library's own `PROVIDE_LOG_FORMAT` (still honoured, so
  driving that stack directly is not cut off).
- `build`, `watch` and `check` now exit `130` (`128 + SIGINT`) when
  interrupted before finishing, instead of the same `1` every other
  failure gets. Previously an operator's own Ctrl-C and a real failure were
  indistinguishable from the exit code alone — and, depending on exactly
  when it landed, the same keypress could exit `0` (`watch`'s steady-state
  loop already treated `ctx.Done()` as a clean stop) or `1` (its own first
  build, or a plain `build`/`check`, returned the raw cancellation as an
  error). See `docs/deployment/operating.md`'s "Exit codes".
- `provenance: true` writes `provenance.json` at the root of `out:` on every
  full build: cairndex's version, a SHA-256 hash of the `cairndex.yaml`
  that drove the run, and a name+digest pair for every file the run wrote,
  closer to an SLSA provenance predicate's `subject` list than to one
  aggregate hash. Off by default, and written only by a full build — see
  `docs/deployment/reference.md`'s "Build provenance" for why `watch`'s incremental
  rebuilds leave an existing manifest as they found it.
- Every listing carries `total_size`: the sum of its own files' bytes, shown
  on the page beside the item count in both presenters and both modes.
  Immediate children only for a directory's own listing, matching `count`'s
  scope — `tree.json`'s is the whole subtree, since its entries are already
  the flattened descendants, with no separate code path to keep in sync.

### Changed

- `docs/deployment.md` is now an overview — the two disk shapes and a map —
  with the depth split into `docs/deployment/`: `static-hosting.md`,
  `container.md`, `operating.md` and `reference.md`. It had grown to 783 lines
  covering everything from nginx config to provenance, which is a reference
  nobody reads twice. The overview keeps its path, so existing links to the
  file still resolve; links that named a section now point at the file holding
  it.
- `docs/deployment/container.md` gains a runbook: choosing a mount shape (the
  container example's `root: ./tree, out: ./site` does not fit a mount pointed
  at a directory that already holds files), what `/healthz` degraded means and
  why it is a readiness signal rather than a liveness one, why a `docker exec
  ... --adopt` repair leaves the endpoint degraded until the watcher itself
  rebuilds, rolling back a build that succeeded, and a reverse proxy in front.

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

- `build`, `watch`, `check` and `serve` now claim SIGTERM's disposition
  alongside SIGINT. Only `os.Interrupt` was registered, so SIGTERM — what
  `docker stop`, `systemctl stop` and a Kubernetes pod shutdown all send — took
  the Go runtime's default action and ended the process where it stood: mid
  build, past the point `SavePartial` would have recorded what had already been
  written, and with the container reporting exit `2`. `docker stop` on a
  long-running `watch --serve` now logs `stopped watching` and exits `0`, which
  is what the exit-code documentation had claimed all along. A SIGTERM that
  interrupts a run reports `130`, the same as a Ctrl-C.

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
- `cairndex.schema.json`'s `override` definition declared `match` as one
  of its own properties, which `defaults:` inherited by `$ref`-ing the
  same definition — an editor's schema check passed `defaults: {match:
  ...}` that `Load` then refused, since `Override` has no `Match` field.
  Fixed with a separate `rule` definition that repeats `override`'s
  properties instead of merging them: draft-07's `additionalProperties` is
  checked per subschema, not across an `allOf`'s union.
- The format switcher had no case for `search`: a directory built with
  `outputs: [html, json, search]` got a genuinely working `search/` page
  with nothing on the listing pointing at it, reachable only by knowing
  the URL in advance.
- `internal/serve/errorpage.go`'s redesign dropped the page's `<h1>` in
  favor of two plain paragraphs — a screen reader had nothing to jump to.
- `outputs: [search]`'s own generated files (`search.js`,
  `search-index.json`, the `search/` page) were not excluded the way
  every other generated name is, so a mirror deployment (`root:` and
  `out:` the same directory) walked its own search output on the next
  build and indexed it, writing a `search/` of its own one level deeper —
  and repeating, one level deeper again, on every rebuild after.
- `search.js` lowercased the haystack but trusted the caller to have
  already lowercased the needle, so a caller passing a raw, mixed-case
  query got a silent miss instead of a match. Separately, a keystroke
  landing just after a failed index fetch could overwrite the "index
  failed to load" message with a plain "0 results", which reads as
  "nothing matched" rather than "nothing loaded".
- The truncated-notice's `index.txt`/`index.json` links on a capped page
  carried no color rule, rendering in the browser's default blue instead
  of the theme.
- `cairndex init`'s cleanup after a failed write compared a file's device
  and inode to decide whether it was still safe to remove — safe only if
  a filesystem never hands a freed inode back to a different file moments
  later. On overlay2 (Docker's default storage driver), it does, which
  could delete a file the cleanup was meant to protect. Writing to a
  private temp file and `Link`-ing it into place instead closes the race
  rather than narrowing it.
