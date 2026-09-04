# SPDX-FileCopyrightText: Copyright (C) 2026 Tim Perkins
# SPDX-License-Identifier: Apache-2.0

GO_COVER_MIN ?= 90.0

.PHONY: pageweight gate lint security test cover example bench tools act act-job clean

gate: lint security test ## Everything a commit must pass

lint: ## gofmt, vet, golangci-lint, file-size budget
	ci/lint.sh

security: ## gosec, govulncheck, go mod verify
	ci/security.sh

test: ## Race-enabled Go suite plus the module's JavaScript
	go test -race ./...
	node --test assets/cairn/cairn.test.mjs

cover: ## Coverage gate over internal/
	go test -race -coverprofile=coverage.out ./internal/...
	ci/check-coverage.sh coverage.out $(GO_COVER_MIN)

act: ## Run the CI workflow locally (ubuntu leg only; needs Docker)
	ci/act.sh

act-job: ## Run one CI job locally, e.g. make act-job JOB=security
	ci/act.sh -j $(JOB)

example: ## End-to-end: cairn build -> hugo -> assert both halves agree
	ci/example.sh

pageweight: ## Assert a directory's page does not grow with the directory
	ci/pageweight.sh

bench: ## Measure one directory at 1k, 10k and 50k entries
	ci/bench.sh

tools: ## Install the gate's binaries
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest

clean:
	rm -f coverage.out
