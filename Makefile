BINARY  := side-eye
GOFLAGS := -buildvcs=false -trimpath
LDFLAGS := -s -w

.PHONY: help install test check scan release

help: ## Show all commands
	@printf '→ side-eye make commands\n\n'
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[1m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf '\n→ Examples:\n\n'
	@printf '    # For a directory\n'
	@printf '    make scan ARGS="~/example/project"\n\n'
	@printf '    # For a GitHub repo\n'
	@printf '    make scan ARGS="example/project"\n\n'
	@printf '    # For a full GitHub repo URL\n'
	@printf '    make scan ARGS="https://github.com/example/project"\n\n'
	@printf '    # For a ZIP file\n'
	@printf '    make scan ARGS="~/Downloads/project-main.zip"\n\n'
	@printf '    # For a release\n'
	@printf '    make release VERSION=1.0.0\n'

install: ## Build the latest code and install the CLI to ~/go/bin for use anywhere
	@printf '→ Installing %s\n' $(BINARY)
	@go install $(GOFLAGS) -ldflags="$(LDFLAGS)" ./cmd/side-eye
	@bin=$$(go env GOBIN); [ -n "$$bin" ] || bin=$$(go env GOPATH)/bin; \
	display=$$(printf '%s' "$$bin" | sed "s|^$$HOME|~|"); \
	printf '✓ Installed to %s\n' "$$display/$(BINARY)"; \
	case ":$$PATH:" in \
		*":$$bin:"*) printf '✓ Ready to use: %s\n' $(BINARY);; \
		*) printf '→ Add %s to your PATH to use %s anywhere\n' "$$display" $(BINARY);; \
	esac

test: ## Run all tests (unit and end-to-end)
	@printf '→ Running unit tests\n'
	@go test $(GOFLAGS) ./internal/...
	@printf '→ Running end-to-end tests\n'
	@go test $(GOFLAGS) ./tests/e2e/...
	@printf '✓ Tests passed\n'

check: ## Format, vet, and test
	@printf '→ Formatting Go files\n'
	@go fmt ./...
	@printf '✓ Formatting applied\n'
	@printf '→ Running go vet\n'
	@go vet $(GOFLAGS) ./...
	@printf '✓ go vet clean\n'
	@printf '→ Running unit tests\n'
	@go test $(GOFLAGS) ./internal/...
	@printf '→ Running end-to-end tests\n'
	@go test $(GOFLAGS) ./tests/e2e/...
	@printf '✓ Tests passed\n'

release: ## Tag and push a release, which starts the release workflow (VERSION="1.0.0")
	@if [ -z "$(VERSION)" ]; then \
		printf '✗ Set the version: make release VERSION=1.0.0\n' >&2; \
		exit 2; \
	fi
	@$(MAKE) --no-print-directory check
	@printf '→ Releasing %s\n' "$(VERSION)"
	@./scripts/release.sh $(VERSION)

scan: ## Scan a directory, GitHub repo, or ZIP (ARGS="...")
	@printf '→ Scanning %s\n' "$(if $(ARGS),$(ARGS),.)"
	@go run $(GOFLAGS) ./cmd/side-eye $(ARGS); code=$$?; \
	if [ $$code -eq 0 ]; then \
		printf '\n✓ Scan clean\n'; \
	elif [ $$code -eq 1 ]; then \
		printf '\n✗ Dangerous configuration found. Review the findings above.\n'; \
	else \
		printf '\n✗ Scan failed. Check the error above.\n'; \
	fi
