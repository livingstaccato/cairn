#!/usr/bin/env bash
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: Apache-2.0
#
# Project-targeted security scans. gosec is the SAST pass; govulncheck compares
# this module's dependency and stdlib versions against the Go vulnerability
# database; `go mod verify` confirms the module cache matches go.sum.
set -euo pipefail

PATH="${PATH}:$(go env GOPATH)/bin"
export PATH

echo "── gosec (SAST)"
gosec -quiet ./...

echo "── govulncheck (known vulnerabilities)"
govulncheck ./...

echo "── go mod verify"
go mod verify

echo "OK: security clean"
