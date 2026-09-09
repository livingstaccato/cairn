#!/usr/bin/env python3
# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Writes a minimal, real, valid wheel for the PEP 503 example fixture. A
# wheel rather than an sdist: pip's `download` can invoke a build backend on
# a sdist even with --no-deps, and this only needs to exist, not build
# anything. Checked in as a generator rather than a committed binary blob so
# the fixture has visible provenance.
#
# Usage: ci/gen_pep503_wheel.py <output-path>
import base64
import hashlib
import sys
import zipfile

NAME = "samplepkg"
VERSION = "1.0.0"


def record_line(path: str, data: bytes) -> str:
    digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
    return f"{path},sha256={digest},{len(data)}"


def main(out_path: str) -> None:
    dist_info = f"{NAME}-{VERSION}.dist-info"
    files = {
        f"{NAME}/__init__.py": b"",
        f"{dist_info}/METADATA": (
            "Metadata-Version: 2.1\n"
            f"Name: {NAME}\n"
            f"Version: {VERSION}\n"
            "Summary: cairndex PEP 503 example fixture\n"
        ).encode(),
        f"{dist_info}/WHEEL": (
            "Wheel-Version: 1.0\n"
            "Generator: cairndex-fixture\n"
            "Root-Is-Purelib: true\n"
            "Tag: py3-none-any\n"
        ).encode(),
    }
    record = [record_line(p, d) for p, d in files.items()]
    record.append(f"{dist_info}/RECORD,,")
    files[f"{dist_info}/RECORD"] = ("\n".join(record) + "\n").encode()

    with zipfile.ZipFile(out_path, "w", zipfile.ZIP_DEFLATED) as zf:
        for path, data in files.items():
            zf.writestr(path, data)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: gen_pep503_wheel.py <output-path>", file=sys.stderr)
        sys.exit(1)
    main(sys.argv[1])
