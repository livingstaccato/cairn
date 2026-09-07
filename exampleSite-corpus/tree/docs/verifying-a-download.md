---
title: Verifying a download
summary: The SHA-256 column is not decoration
tags: [guide, integrity]
---

Every row in `/downloads/` links its digest to `SHA256SUMS`, in coreutils
format — `sha256sum -c SHA256SUMS` verifies the whole directory against it,
no bespoke tool required.
