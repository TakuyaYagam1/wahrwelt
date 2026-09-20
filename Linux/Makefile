SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

LINUX_ROOT := $(abspath .)
REPO_ROOT := $(abspath ..)
NIXOS_DIR := $(abspath NixOS)
DOTS_DIR := $(abspath dots)
INSTALLER_DIR := $(abspath installer)

NIX_RUN := nix run --no-write-lock-file nixpkgs\#

STATIX := $(NIX_RUN)statix --
DEADNIX := $(NIX_RUN)deadnix --
SHFMT := $(NIX_RUN)shfmt --
STYLUA := $(NIX_RUN)stylua --

# find -print0 chains keep paths with spaces intact (e.g. NixOS/home/noctalia/.../Catppuccin Lavender/).
NIX_FIND := find $(NIXOS_DIR) -type f -name '*.nix' -print0
SHELL_FIND := find $(DOTS_DIR)/hypr/scripts -type f -name '*.sh' -print0
SHELL_LINT_DIRS := Linux/dots/hypr/scripts Linux/NixOS
FISH_FIND := find $(DOTS_DIR)/hypr/scripts -type f -name '*.fish' -print0
LUA_DIRS := $(DOTS_DIR)/nvim $(DOTS_DIR)/hypr
LUA_FIND := find $(LUA_DIRS) -type f -name '*.lua' -print0
JSON_FIND := find $(NIXOS_DIR) $(DOTS_DIR) $(INSTALLER_DIR)/internal/shellruntime -type f -name '*.json' ! -name 'lazy-lock.json' -print0
PYTHON_FIND := find $(NIXOS_DIR) $(DOTS_DIR)/hypr/scripts -type f -name '*.py' -print0

##@ Aggregate

.PHONY: lint
lint: lint-go lint-nix lint-shell lint-lua lint-json lint-python ## Run all linters across go/nix/shell/lua/json/python

.PHONY: fmt-check
fmt-check: fmt-check-go fmt-check-nix fmt-check-shell fmt-check-lua ## Check formatting without writing

.PHONY: fmt
fmt: fmt-go fmt-nix fmt-shell fmt-lua ## Apply formatting in place across go/nix/shell/lua

.PHONY: test
test: test-go test-hypr-integration ## Run all Go and Hypr integration test suites

.PHONY: vuln
vuln: vuln-go ## Run all vulnerability scanners

.PHONY: ci
ci: lint fmt-check test vuln ## Full CI pipeline: lint + fmt-check + test + vuln

.PHONY: all
all: lint test ## Lint + test (fast feedback loop)

##@ Go (installer)

.PHONY: lint-go
lint-go: ## golangci-lint on Linux/installer
	$(MAKE) -C $(INSTALLER_DIR) lint

.PHONY: lint-go-fix
lint-go-fix: ## golangci-lint --fix on Linux/installer
	$(MAKE) -C $(INSTALLER_DIR) lint-fix

.PHONY: fmt-go
fmt-go: ## golangci-lint fmt on Linux/installer
	$(MAKE) -C $(INSTALLER_DIR) fmt

.PHONY: fmt-check-go
fmt-check-go: ## Check Go formatting without writing
	$(MAKE) -C $(INSTALLER_DIR) fmt-check

.PHONY: vet-go
vet-go: ## go vet ./... in installer
	$(MAKE) -C $(INSTALLER_DIR) vet

.PHONY: test-go
test-go: ## Run installer test suite (race detector)
	$(MAKE) -C $(INSTALLER_DIR) test

.PHONY: vuln-go
vuln-go: ## govulncheck on installer module
	cd $(INSTALLER_DIR) && govulncheck ./...

##@ Nix (NixOS/)

.PHONY: lint-nix
lint-nix: lint-nix-statix lint-nix-deadnix ## statix + deadnix

.PHONY: lint-nix-statix
lint-nix-statix: ## statix anti-pattern check
	$(STATIX) check $(NIXOS_DIR)

.PHONY: lint-nix-deadnix
lint-nix-deadnix: ## deadnix dead-code check (fails on findings)
	$(DEADNIX) --fail $(NIXOS_DIR)

.PHONY: fmt-nix
fmt-nix: ## nixfmt-tree across NixOS/
	cd $(REPO_ROOT) && nix fmt Linux/NixOS

.PHONY: fmt-check-nix
fmt-check-nix: ## Check Nix formatting with nixfmt-tree
	cd $(REPO_ROOT) && nix fmt -- --ci Linux/NixOS

.PHONY: nix-syntax
nix-syntax: ## Parse all .nix files for syntax errors
	@count=0; while IFS= read -r -d '' f; do \
		nix-instantiate --parse "$$f" >/dev/null || { echo "FAIL: $$f"; exit 1; }; \
		count=$$((count+1)); \
	done < <($(NIX_FIND)); \
	echo "nix-syntax: $$count .nix files parsed cleanly"

.PHONY: nix-flake-check
nix-flake-check: ## Run nix flake check (full eval)
	cd $(NIXOS_DIR) && nix flake check --no-write-lock-file

.PHONY: nix-build
nix-build: ## Delegate to installer Makefile nix-build target
	$(MAKE) -C $(INSTALLER_DIR) nix-build

.PHONY: nix-package-preset-contract
nix-package-preset-contract: ## Check cumulative package presets and desktop file handlers
	$(MAKE) -C $(INSTALLER_DIR) nix-package-preset-contract

##@ Dotfiles (shell, fish, lua, json)

.PHONY: lint-shell
lint-shell: lint-shell-bash lint-shell-fish ## shellcheck + fish -n

.PHONY: lint-shell-bash
lint-shell-bash: ## bash -n syntax + shellcheck -x (CWD=repo root for source= follow)
	@cd $(REPO_ROOT) && find $(SHELL_LINT_DIRS) -type f -name '*.sh' -print0 | xargs -0 -r -n1 bash -n
	@cd $(REPO_ROOT) && find $(SHELL_LINT_DIRS) -type f -name '*.sh' -print0 | xargs -0 -r shellcheck -x

.PHONY: lint-shell-fish
lint-shell-fish: ## fish -n on dots/hypr/scripts/*.fish
	@$(FISH_FIND) | xargs -0 -r -n1 fish -n

.PHONY: fmt-shell
fmt-shell: ## shfmt -w on dots/hypr/scripts/*.sh
	@$(SHELL_FIND) | xargs -0 -r $(SHFMT) -w -i 2 -ci

.PHONY: fmt-check-shell
fmt-check-shell: ## shfmt -d (diff) on dots/hypr/scripts/*.sh
	@$(SHELL_FIND) | xargs -0 -r $(SHFMT) -d -i 2 -ci

.PHONY: lint-lua
lint-lua: ## stylua --check on Neovim and Hypr Lua configs
	@if $(LUA_FIND) | grep -qz .; then $(STYLUA) --check $(LUA_DIRS); fi

.PHONY: fmt-check-lua
fmt-check-lua: lint-lua ## Check Lua formatting without writing

.PHONY: fmt-lua
fmt-lua: ## stylua write on Neovim and Hypr Lua configs
	@if $(LUA_FIND) | grep -qz .; then $(STYLUA) $(LUA_DIRS); fi

.PHONY: lint-json
lint-json: ## jq empty on every JSON in NixOS/, dots/, installer manifests
	@count=0; while IFS= read -r -d '' f; do \
		jq empty "$$f" >/dev/null || { echo "FAIL: $$f"; exit 1; }; \
		count=$$((count+1)); \
	done < <($(JSON_FIND)); \
	echo "lint-json: $$count .json files parse cleanly"

.PHONY: lint-python
lint-python: ## python ast.parse on NixOS helpers, patch sources, and Hypr tests
	@$(PYTHON_FIND) | xargs -0 -r python3 -c 'import ast, pathlib, sys; [ast.parse(pathlib.Path(path).read_text()) for path in sys.argv[1:]]'

##@ Project-wide checks

.PHONY: shell-contracts
shell-contracts: ## Hypr keybind/profile contract tests
	$(MAKE) -C $(INSTALLER_DIR) hypr-bind-check

.PHONY: test-hypr-integration
test-hypr-integration: ## Shell transitions, app-close behavior, Lua runtime, and selector model
	$(MAKE) -C $(INSTALLER_DIR) hypr-integration-check

.PHONY: nix-end4-hypr-build
nix-end4-hypr-build: ## Realize and inspect the validated End4 Hypr artifact
	$(MAKE) -C $(INSTALLER_DIR) nix-end4-hypr-build

.PHONY: nix-end4-pc-quickshell-build
nix-end4-pc-quickshell-build: ## Realize and inspect the managed End4 pC QuickShell artifact
	$(MAKE) -C $(INSTALLER_DIR) nix-end4-pc-quickshell-build

.PHONY: nix-shell-transition-build
nix-shell-transition-build: ## Realize and inspect the managed QuickShell transition artifact
	$(MAKE) -C $(INSTALLER_DIR) nix-shell-transition-build

.PHONY: full-check
full-check: ## Mirror installer/Makefile check target (full local CI)
	$(MAKE) -C $(INSTALLER_DIR) check

##@ Help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN{FS=":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z0-9_-]+:.*?##/ {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5)}' $(MAKEFILE_LIST)
