# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: Apache-2.0
"""Fail if any internal link in the built site does not resolve.

A listing whose entries 404 is not a listing. This runs against the published
tree *after* the artifact overlay, so it checks what a visitor actually gets
rather than what Hugo alone produced.
"""

import os
import pathlib
import re
import sys

EXTERNAL = ("http://", "https://", "#", "mailto:")


def main(root_arg: str) -> int:
    root = pathlib.Path(root_arg)
    bad = set()
    for page in sorted(root.rglob("*.html")):
        html = page.read_text(errors="ignore")
        for href in re.findall(r'href="([^"]+)"', html):
            if href.startswith(EXTERNAL):
                continue
            base = root / href.lstrip("/") if href.startswith("/") else page.parent / href
            target = pathlib.Path(os.path.normpath(base))
            if not (target.exists() or (target / "index.html").exists()):
                bad.add(f"{page}: {href}")
    if bad:
        print("FAIL: broken links")
        for b in sorted(bad):
            print("  " + b)
        return 1
    print("OK: every internal link resolves")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
