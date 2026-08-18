.DEFAULT_GOAL := help

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

.PHONY: lint
lint: ## Lint sources with ruff and audit the workflows with zizmor
	@uv run ruff check .
	@uv run zizmor .github/workflows

.PHONY: format
format: ## Auto-format with ruff
	@uv run ruff format .

.PHONY: test
test: ## Run the test suite
	@uv run pytest

.PHONY: build
build: ## Build the wheel
	@uv build --wheel

.PHONY: clean
clean: ## Remove build artifacts
	@rm -rf dist
