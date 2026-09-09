# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: MIT
#
# Three independent things live in this file, as separate build targets:
#
#   gate    - the CI gate (lint, security, Go+JS tests, the Hugo end-to-end),
#             run in a container pinned to the exact toolchain versions
#             .github/workflows/ci.yml uses, instead of whatever Go/Hugo/Node
#             happen to be on a given machine. `docker build --target gate .`
#             fails the build the same way a failing `make gate` does.
#   runtime - the composed example site (cairndex build -> hugo -> artifact
#             overlay, exactly as ci/example.sh assembles it) on a minimal
#             image with no Go, Hugo or Node at all. Run it with
#             `--network none` (see ci/docker-airgap.sh) to check the
#             "zero external runtime assets, works airgapped" claim in
#             AGENTS.md by actually removing the network, not by grepping
#             the output for CDN URLs.
#             gate and runtime are CI/demo tooling, not for anyone else to
#             run -- do not repurpose either for real use; see server below.
#   server  - the cairndex binary alone, running `watch --serve` against a
#             mounted volume: an actual single-container deployment, not a
#             demo. See docs/deployment.md's "Run as a container" section
#             for what this is (and is not) a replacement for.
#
# `docker build` with no --target builds runtime, since that's the one meant
# to be run rather than just checked, and predates server as this file's
# default. Building the container someone would actually run always needs an
# explicit --target server.

ARG GO_VERSION=1.26
ARG HUGO_VERSION=0.165.0
ARG NODE_VERSION=22
ARG GOLANGCI_LINT_VERSION=v2.12.2
ARG GOSEC_VERSION=v2.29.0
ARG GOVULNCHECK_VERSION=v1.7.0

FROM golang:${GO_VERSION}-bookworm AS toolchain
ARG HUGO_VERSION
ARG NODE_VERSION
ARG GOLANGCI_LINT_VERSION
ARG GOSEC_VERSION
ARG GOVULNCHECK_VERSION
# TARGETARCH is set automatically by buildkit; Hugo's own release asset names
# use the same amd64/arm64 spelling, so no translation table is needed.
ARG TARGETARCH

# curl, to fetch the pinned Hugo release and the NodeSource setup script.
RUN apt-get update && apt-get install -y --no-install-recommends \
      curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Debian bookworm's own nodejs package is not v22; setup-node@v4 in ci.yml
# pins v22, so this does the same via NodeSource rather than drifting.
RUN curl -fsSL "https://deb.nodesource.com/setup_${NODE_VERSION}.x" | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL \
      "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-${TARGETARCH}.tar.gz" \
    | tar -xz -C /usr/local/bin hugo

# The gate's own binaries, pinned to the same versions ci.yml installs.
RUN go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}" \
    && go install "github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION}" \
    && go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY package.json package-lock.json ./
RUN npm ci

COPY . .

# ---- gate: everything a commit must pass, plus the Hugo end-to-end -------
FROM toolchain AS gate
# private (ci/check-private.sh) needs a secret pattern file this image has no
# business holding, and reads git history this build context does not carry
# (.dockerignore drops .git) -- it stays a git-hook/CI-secret concern, not a
# container one. Everything else `make gate` and the example/templates CI
# jobs run, this does too.
RUN ci/lint.sh
RUN ci/security.sh
RUN go test -race ./...
RUN node --test assets/cairndex/*.test.mjs
RUN ci/example.sh
RUN ci/example-corpus.sh
RUN ci/templates.sh
RUN ci/pageweight.sh

# ---- build: the composed example site, nothing else ----------------------
FROM toolchain AS build
RUN go run ./cmd/cairndex build --config exampleSite/cairndex.yaml \
    && cd exampleSite && hugo --quiet
# The demo overlay ci/example.sh also does: Hugo only ever sees the
# _index.md files cairndex wrote, never the artifact tree itself, so the
# tree is copied over the published site to make one root serve both. A real
# deployment does this by pointing a real web server's document root at the
# artifact tree with the Hugo output layered in, not by copying files.
RUN cp -R exampleSite/tree/. exampleSite/public/

# ---- runtime: no Go, no Hugo, no Node, nothing but the served files ------
FROM busybox:1.36 AS runtime
# Documents the container's own listen port; overridden at `docker run` with
# -p, same as any other container. Not the cairndex serve default (22476,
# internal/serve.DefaultAddr) -- this is a plain static server over Hugo's
# output, the thing ci/example.sh itself checks, and cairndex serve does not
# render Hugo templates at all (see ci/docker-airgap.sh).
ARG DEMO_PORT=8080
ENV DEMO_PORT=${DEMO_PORT}
COPY --from=build /src/exampleSite/public /srv/public
EXPOSE ${DEMO_PORT}
CMD ["sh", "-c", "httpd -f -v -p ${DEMO_PORT} -h /srv/public"]

# ---- server: cairndex itself, for a real long-lived single container -----
# CGO_ENABLED=0: a static binary is what lets the final image be busybox
# rather than needing glibc. cairndex makes no outbound network calls in
# watch/serve (no cgo resolver ever needed either), so nothing is lost.
FROM toolchain AS server-build
RUN CGO_ENABLED=0 go build -o /out/cairndex ./cmd/cairndex

FROM busybox:1.36 AS server
COPY --from=server-build /out/cairndex /usr/local/bin/cairndex
# --addr 0.0.0.0 is required here and is not internal/serve.DefaultAddr
# (127.0.0.1:22476) on purpose -- that default is for local preview on the
# machine that ran it, unreachable from outside a container's own network
# namespace otherwise. mode: hugo will not work in this image: there is no
# Hugo here, only the cairndex binary, so /data/cairndex.yaml must be a
# mode: direct config -- exactly the shape `cairndex init` itself writes.
EXPOSE 8080
VOLUME /data
WORKDIR /data
ENTRYPOINT ["cairndex"]
CMD ["watch", "--serve", "--addr", "0.0.0.0:8080"]
