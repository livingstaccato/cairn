#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: Apache-2.0
#
# Formatting, vet, static analysis and the file-size budget.
set -euo pipefail

# go install puts binaries in GOPATH/bin, which is not on PATH for
# non-interactive shells (pre-commit hooks, CI steps that skip the profile).
PATH="${PATH}:$(go env GOPATH)/bin"
export PATH

echo "── gofmt"
unformatted="$(gofmt -l . || true)"
if [ -n "${unformatted}" ]; then
  echo "FAIL: not gofmt-clean:"
  echo "${unformatted}"
  exit 1
fi

echo "── go vet"
go vet ./...

echo "── golangci-lint"
golangci-lint run

echo "── file size budget"
ci/check-max-loc.sh 777

echo "OK: lint clean"
