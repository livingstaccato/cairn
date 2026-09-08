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
  directory does not actually hold only projects or only files.

### Changed

- `cairndex build` and `cairndex check` now stop promptly when interrupted
  mid-run, instead of only surviving a Ctrl-C to keep going. A build cut
  short still saves a manifest for what it wrote.

### Fixed

- The breadcrumb partial built its trail from a manual `base_path` string
  trim; a directory this disagreed with the site's real mount rendered
  broken links. It now walks Hugo's own page ancestors instead.
- `cairndex serve` opened and re-resolved a request path twice, widening the
  window a concurrent writer could swap a symlink into; it now opens the
  path containment already resolved.
