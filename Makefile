BINARY := fft
PKG    := ./cmd/fft

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the fft binary
	go build -trimpath -o $(BINARY) $(PKG)

.PHONY: generate
generate: ## Regenerate the API client and the operation metadata from the OpenAPI spec
	go generate ./...

.PHONY: docs
docs: ## Regenerate the documentation-site sources (guide pages + CLI reference)
	go run ./tools/docsgen -out docs/guide
	go run $(PKG) gen-docs docs/reference/commands

.PHONY: test
test: ## Run all specs with the race detector
	go test -race -shuffle=on ./...

.PHONY: lint
lint: ## Run go vet and golangci-lint
	go vet ./...
	golangci-lint run

.PHONY: vulncheck
vulncheck: ## Scan dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

.PHONY: snapshot
snapshot: ## Build all release targets locally, exactly as the tag build will
	goreleaser build --snapshot --clean

.PHONY: fmt
fmt: ## Format the code
	gofmt -s -w .

.PHONY: tidy
tidy: ## Tidy go.mod
	go mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf dist/ $(BINARY)
