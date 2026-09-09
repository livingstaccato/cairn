#!/usr/bin/env python3
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Verifies a published tree against its own provenance.json: the real
# SHA-256 of every file it names, not just that the JSON parses. The same
# "verify against the real tool" standard SHA256SUMS and the PEP 503
# fixture already get in ci/example.sh, closing the loop on what
# provenance.json is actually for.
#
# Usage: ci/verify_provenance.py <published-dir>
import hashlib
import json
import sys
from pathlib import Path


def main(pub_dir: str) -> int:
    manifest = json.loads((Path(pub_dir) / "provenance.json").read_text())

    fail = 0
    for entry in manifest["outputs"]:
        path = Path(pub_dir) / entry["path"]
        if not path.is_file():
            print(f"FAIL: {entry['path']} is in provenance.json but missing from {pub_dir}")
            fail = 1
            continue
        got = hashlib.sha256(path.read_bytes()).hexdigest()
        if got != entry["sha256"]:
            print(f"FAIL: {entry['path']} sha256 is {got}, provenance.json says {entry['sha256']}")
            fail = 1

    if not fail:
        print(f"OK: {len(manifest['outputs'])} outputs match provenance.json")
    return fail


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: verify_provenance.py <published-dir>", file=sys.stderr)
        sys.exit(1)
    sys.exit(main(sys.argv[1]))
