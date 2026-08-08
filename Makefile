.DEFAULT_GOAL := help

BINARY := src/pdfmasker/_bin/masker

.DEFAULT:
	@echo "No such command (or you passed two or more targets to make). List of possible commands: make help"
	@exit 2

.DEFAULT_GOAL := help

##@ Help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target> <arg=value>\033[0m\n"} /^[a-zA-Z0-9._-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m  %s\033[0m\n\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: init
init: ## Create the local virtualenv and install the git hooks
	@uv venv -p 3.12
	@uv run prek install

.PHONY: binary
binary: ## Build the Go binary into the package for local iteration
	@go build -trimpath -o $(BINARY) ./cmd/masker

.PHONY: lint
lint: ## Lint sources with ruff
	@uv run ruff check .
	@uv run zizmor .github/workflows

.PHONY: format
format: ## Auto-format Python (ruff) and Go (gofmt)
	@uv run ruff format .
	@go fmt ./...

.PHONY: test-go
test-go: ## Run the Go engine tests (vet + race detector)
	@go vet ./...
	@go test ./internal/... -race

.PHONY: test-py
test-py: ## Run the Python end-to-end tests (recompiles the binary)
	@uv run pytest

.PHONY: test
test: test-go test-py ## Run the full test suite (Go + Python)

.PHONY: build
build: ## Build the native platform wheel
	@uv build --wheel

.PHONY: clean
clean: ## Remove build artifacts and the compiled binary
	@rm -rf dist src/pdfmasker/_bin
