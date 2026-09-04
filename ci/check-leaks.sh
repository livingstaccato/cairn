#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Refuse shapes of data that are never intentional in a public repository:
# email addresses, developer home paths, and private-network addresses.
#
# This needs no configuration, so it runs everywhere including CI. The
# term-specific companion, check-private.sh, needs a word list that would
# itself be sensitive, so it stays local. Between them: this catches the
# generic shapes on every push, that one catches the specific names on every
# commit made on a machine that has the list.
#
# Everything here ships inside a Go module zip, and a published version cannot
# be withdrawn.
set -euo pipefail

cd "$(dirname "$0")/.."

fail=0
report() {
  local what="$1" hits="$2"
  if [ -n "$hits" ]; then
    echo "check-leaks: $what:"
    printf '%s\n' "$hits" | sed 's/^/  /'
    fail=1
  fi
}

# Locations only, never the match itself: this log is public.
loc() { cut -d: -f1,2; }

# Email addresses. example.com / example.invalid / example.org are RFC 2606
# and RFC 6761 reserved and cannot belong to anyone.
report "email addresses" "$(
  git grep -nIE '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' -- . 2>/dev/null \
    | grep -vE '@example\.(com|org|net|invalid)' \
    | grep -vE '@[A-Za-z0-9.-]*\.(invalid|test|example|localhost)\b' \
    | loc || true)"

# A developer's home directory. Leaks a username and a machine layout.
report "home directory paths" "$(
  git grep -nIE '/(Users|home)/[A-Za-z0-9._-]+' -- . 2>/dev/null | loc || true)"

# RFC 1918 and loopback literals. Real infrastructure addressing.
# \b is a GNU extension that git grep's POSIX engine ignores on macOS without
# error, so the boundaries are spelled out to behave the same on both.
report "private network addresses" "$(
  git grep -nIE '(^|[^0-9.])(10\.[0-9]{1,3}|192\.168|172\.(1[6-9]|2[0-9]|3[01]))\.[0-9]{1,3}\.[0-9]{1,3}([^0-9.]|$)' -- . 2>/dev/null | loc || true)"

[ "$fail" -eq 0 ] || exit 1
echo "check-leaks: clean"
