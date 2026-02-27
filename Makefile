# Makefile for try (Go)

SHELL := /bin/bash
GO := go
BINARY := try
CMD := ./cmd/try

.PHONY: help
help: ## Show this help message
	@echo "try - Fresh directories for every vibe"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

.PHONY: test
test: ## Run all tests
	@$(GO) test ./...

.PHONY: lint
lint: ## Run go fmt check
	@test -z "$$(gofmt -l cmd/try/*.go)" || (echo "Run gofmt on changed files" && exit 1)

.PHONY: build
build: ## Build binary
	@$(GO) build -o $(BINARY) $(CMD)

.PHONY: install
install: build ## Install binary to ~/.local/bin
	@mkdir -p ~/.local/bin
	@cp $(BINARY) ~/.local/bin/$(BINARY)
	@chmod +x ~/.local/bin/$(BINARY)
	@echo "Installed: ~/.local/bin/$(BINARY)"

.PHONY: version
version: ## Show version information
	@$(GO) run $(CMD) --version

.PHONY: clean
clean: ## Clean build artifacts
	@rm -f $(BINARY)

.PHONY: check-deps
check-deps: ## Check required dependencies
	@command -v $(GO) >/dev/null 2>&1 || { echo "Go is required but not installed"; exit 1; }
	@echo "✓ Go found: $$($(GO) version)"
	@command -v git >/dev/null 2>&1 || { echo "Git is required but not installed"; exit 1; }
	@echo "✓ Git found: $$(git --version)"

.PHONY: dev-setup
dev-setup: check-deps ## Set up development environment
	@echo "Run: make test && make build"

.PHONY: all
all: lint test build ## Run all checks

.PHONY: t
t: test ## Shortcut for test

.PHONY: l
l: lint ## Shortcut for lint

.PHONY: i
i: install ## Shortcut for install
