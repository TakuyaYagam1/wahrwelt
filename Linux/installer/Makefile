SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

# Pinned tool versions.
GOLANGCI_LINT_VERSION ?= v2.11.4

GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
NIXOS_DIR := $(abspath ../NixOS)
REPO_ROOT := $(abspath ../..)
NIX_CACHE_SUBSTITUTERS := https://nix-community.cachix.org https://hyprland.cachix.org https://quickshell.cachix.org https://numtide.cachix.org
NIX_CACHE_KEYS := nix-community.cachix.org-1:mB9FSh9qf2dCimDSUo8Zy7bkq5CX+/rkCWyvRCYg3Fs= hyprland.cachix.org-1:a7pgxzMz7+chwVL3/pzj6jIBMioiJM7ypFP8PwtkuGc= quickshell.cachix.org-1:tjWMR3PQd01gN6YtjSRUdHHHUgrSLFIgwqrCQjFXVOU= numtide.cachix.org-1:2ps1kLBUWjxIneOy1Ik6cQjb41X0iXVXeHigGmycPPE=
NIX_CACHE_ARGS := --option extra-substituters "$(NIX_CACHE_SUBSTITUTERS)" --option extra-trusted-public-keys "$(NIX_CACHE_KEYS)"
NIX_RUN := nix run $(NIX_CACHE_ARGS) --no-write-lock-file
LUA := $(NIX_RUN) nixpkgs\#lua --
NODE := $(NIX_RUN) nixpkgs\#nodejs --
HYPR_SHELL_INTEGRATION_TESTS := \
	Linux/dots/hypr/scripts/tests/end4-artifact-verifier-test.sh \
	Linux/dots/hypr/scripts/tests/end4-ownership-test.sh \
	Linux/dots/hypr/scripts/tests/end4-variants-test.sh \
	Linux/dots/hypr/scripts/tests/hypr-source-contract-test.sh \
	Linux/dots/hypr/scripts/tests/snapshot-ownership-test.sh \
	Linux/dots/hypr/scripts/tests/shell-runtime-dir-test.sh \
	Linux/dots/hypr/scripts/tests/shell-runtime-env-test.sh \
	Linux/dots/hypr/scripts/tests/shell-selector-lock-test.sh \
	Linux/dots/hypr/scripts/tests/shell-process-lifecycle-test.sh \
	Linux/dots/hypr/scripts/tests/noctalia-process-test.sh \
	Linux/dots/hypr/scripts/tests/close-active-test.sh \
	Linux/dots/hypr/scripts/tests/shell-transition-overlay-test.sh \
	Linux/dots/hypr/scripts/tests/shell-transition-fast-path-test.sh \
	Linux/dots/hypr/scripts/tests/shell-transition-test.sh

END4_HYPR_EXPR := let \
  nixosDir = builtins.getEnv "WAHRWELT_NIXOS_DIR"; \
  flake = builtins.getFlake ("path:" + nixosDir); \
  system = "x86_64-linux"; \
  pkgs = import flake.inputs.nixpkgs { inherit system; config.allowUnfree = true; }; \
  lib = pkgs.lib; \
  wahrweltLib = import (nixosDir + "/lib/mysetup.nix") { inherit lib; }; \
  pkgsStable = import flake.inputs.nixpkgs-stable { \
    localSystem = system; \
    config.allowUnfree = true; \
    config.permittedInsecurePackages = [ "python3.12-pypdf2-3.0.1" ]; \
  }; \
  wahrwelt = import (nixosDir + "/hosts/NixOS/host-vars.nix"); \
  hm = flake.inputs.home-manager.lib.homeManagerConfiguration { \
    inherit pkgs; \
    modules = [ (nixosDir + "/home/home.nix") ]; \
    extraSpecialArgs = { \
      inputs = flake.inputs; \
      inherit wahrweltLib wahrwelt; \
      mysetupLib = wahrweltLib; \
      mysetup = wahrwelt; \
      pkgs-stable = pkgsStable; \
    }; \
  }; \
in hm.config.xdg.configFile."hypr/end4".source

END4_PC_QUICKSHELL_EXPR := $(subst hm.config.xdg.configFile."hypr/end4".source,hm.config.xdg.configFile."quickshell/end4-pC".source,$(END4_HYPR_EXPR))
SHELL_TRANSITION_EXPR := $(subst hm.config.xdg.configFile."hypr/end4".source,hm.config.xdg.configFile."quickshell/wahrwelt-shell-transition".source,$(END4_HYPR_EXPR))

##@ Build & run

.PHONY: build
build: ## Build wahrwelt and the supported mysetup compatibility alias
	@mkdir -p bin
	go build -trimpath -o bin/wahrwelt ./cmd/wahrwelt
	go build -trimpath -o bin/wahrwelt-fs-helper ./cmd/wahrwelt-fs-helper
	ln -sfn wahrwelt bin/mysetup

.PHONY: install
install: build ## Install wahrwelt and the supported mysetup compatibility alias into $$HOME/.local/bin
	@install -d "$$HOME/.local/bin"
	install -m 0755 bin/wahrwelt "$$HOME/.local/bin/wahrwelt"
	install -m 0755 bin/wahrwelt-fs-helper "$$HOME/.local/bin/wahrwelt-fs-helper"
	ln -sfn wahrwelt "$$HOME/.local/bin/mysetup"
	@echo "installed: $$HOME/.local/bin/wahrwelt (supported compatibility alias: mysetup)"

.PHONY: run
run: ## Run the installer
	go run ./cmd/wahrwelt

.PHONY: dev-deps
dev-deps: ## Download module dependencies into the local cache
	go mod download

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: clean
clean: ## Remove build and coverage artefacts
	rm -rf bin/ tmp/ coverage.out coverage.html

##@ Lint, format, test

.PHONY: lint
lint: ## golangci-lint run
	$(GOLANGCI_LINT) run ./...

.PHONY: lint-fix
lint-fix: ## golangci-lint run --fix
	$(GOLANGCI_LINT) run --fix ./...

.PHONY: fmt
fmt: ## golangci-lint fmt
	$(GOLANGCI_LINT) fmt

.PHONY: fmt-check
fmt-check: ## Check Go formatting without rewriting files
	$(GOLANGCI_LINT) fmt --diff

.PHONY: vet
vet: ## go vet ./...
	go vet ./...

.PHONY: shell-check
shell-check: ## Check Hypr shell scripts and JSON/Python patch sources
	cd $(REPO_ROOT) && find Linux/dots/hypr/scripts -type f -name '*.sh' -print0 | xargs -0 -r -n1 bash -n
	cd $(REPO_ROOT) && find Linux/dots/hypr/scripts -type f -name '*.sh' -print0 | xargs -0 -r shellcheck -x
	cd $(REPO_ROOT) && bash -n Linux/NixOS/home/end4/patches/first-frame-thumbnails.sh
	cd $(REPO_ROOT) && shellcheck -x Linux/NixOS/home/end4/patches/first-frame-thumbnails.sh
	cd $(REPO_ROOT) && bash -n Linux/NixOS/home/end4/patches/video-backend-reconcile.sh
	cd $(REPO_ROOT) && shellcheck -x Linux/NixOS/home/end4/patches/video-backend-reconcile.sh
	cd $(REPO_ROOT) && fish -n Linux/dots/hypr/scripts/wsaction.fish
	cd $(REPO_ROOT) && find Linux/NixOS/home/end4 -type f -name '*.json' -print0 | xargs -0 -r -n1 jq empty
	cd $(REPO_ROOT) && if find Linux/NixOS/home/end4/patches \( -type f \( -name '*.pyc' -o -name '*.pyo' \) -o -type d -name '__pycache__' \) -print -quit | grep -q .; then echo 'FAIL: generated Python artifacts in End4 patches'; exit 1; fi
	cd $(REPO_ROOT) && python3 -c 'import ast, pathlib; [ast.parse(pathlib.Path(path).read_text()) for path in ("Linux/NixOS/home/end4/patches/quickshell.py", "Linux/NixOS/home/end4/patches/quickshell-pc.py", "Linux/NixOS/home/end4/patches/live-wallpapers.py", "Linux/NixOS/home/end4/patches/media-index.py", "Linux/dots/hypr/scripts/tests/end4-artifact-lifecycle.py")]'

.PHONY: hypr-bind-check
hypr-bind-check: ## Check Hypr keybind contracts and duplicate chords
	go test -count=1 ./internal/dots -run 'Test(HyprKeybindFilesDoNotContainUnexpectedDuplicateChords|ShellKeybindProfilesUseExpectedLaunchers)'

.PHONY: hypr-shell-integration-check
hypr-shell-integration-check: ## Run deterministic Hypr shell lifecycle and app-close integration tests
	@status=0; \
	for test_script in $(HYPR_SHELL_INTEGRATION_TESTS); do \
		(cd $(REPO_ROOT) && bash "$$test_script") || status=$$?; \
	done; \
	exit $$status

.PHONY: hypr-lua-contract-check
hypr-lua-contract-check: ## Check canonical Lua load order and End4 adapter restoration
	cd $(REPO_ROOT) && $(LUA) Linux/dots/hypr/tests/lua-contract-test.lua
	cd $(REPO_ROOT) && $(LUA) Linux/dots/hypr/scripts/tests/end4-close-bind-test.lua Linux/dots/hypr Linux/dots/hypr/end4/keybinds.lua

.PHONY: shell-selector-model-check
shell-selector-model-check: ## Check shell selector family and End4 variant behavior
	cd $(REPO_ROOT) && $(NODE) Linux/NixOS/home/shells/quickshell/wahrwelt-shell-selector/tests/selector-model.test.mjs

.PHONY: shell-transition-model-check
shell-transition-model-check: ## Check exact 3 + 3 + 3 and End4 3 + 5 + 3 shell transition timelines
	cd $(REPO_ROOT) && $(NODE) Linux/NixOS/home/shells/quickshell/wahrwelt-shell-transition/tests/transition-model.test.mjs

.PHONY: hypr-integration-check
hypr-integration-check: ## Run shell, Lua, selector, and transition integration gates
	@status=0; \
	$(MAKE) hypr-shell-integration-check || status=$$?; \
	$(MAKE) hypr-lua-contract-check || status=$$?; \
	$(MAKE) shell-selector-model-check || status=$$?; \
	$(MAKE) shell-transition-model-check || status=$$?; \
	exit $$status

.PHONY: test
test: ## Unit tests with race detector
	go test -race -count=1 ./...

.PHONY: cover
cover: ## Unit-test coverage report (HTML)
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

##@ Nix

.PHONY: nix-build
nix-build: ## Build Wahrwelt and its supported mysetup flake compatibility alias
	nix build $(NIX_CACHE_ARGS) --no-link --no-write-lock-file "path:$(REPO_ROOT)?dir=Linux/NixOS#wahrwelt"
	nix build $(NIX_CACHE_ARGS) --no-link --no-write-lock-file "path:$(REPO_ROOT)?dir=Linux/NixOS#mysetup"

.PHONY: nix-package-eval
nix-package-eval: ## Eval Wahrwelt and its supported mysetup package compatibility alias
	nix eval $(NIX_CACHE_ARGS) --no-write-lock-file --impure --raw --expr 'let flake = builtins.getFlake "path:$(REPO_ROOT)?dir=Linux/NixOS"; in flake.packages.x86_64-linux.wahrwelt.pname' >/dev/null
	nix eval $(NIX_CACHE_ARGS) --no-write-lock-file --impure --raw --expr 'let flake = builtins.getFlake "path:$(REPO_ROOT)?dir=Linux/NixOS"; in flake.packages.x86_64-linux.mysetup.pname' >/dev/null

NIX_HM_ENV := NIXOS_DIR='$(NIXOS_DIR)' \
	NIX_CACHE_SUBSTITUTERS='$(NIX_CACHE_SUBSTITUTERS)' \
	NIX_CACHE_KEYS='$(NIX_CACHE_KEYS)'

.PHONY: nix-noctalia-hm-compat
nix-noctalia-hm-compat: ## Eval reusable Noctalia module ownership across Home Manager versions
	cd $(REPO_ROOT) && bash Linux/installer/scripts/noctalia-hm-module-compat-test.sh

.PHONY: nix-hm-eval
nix-hm-eval: ## Eval runtime shell module and all-on Home Manager imports, including End4 Official/pC
	@$(NIX_HM_ENV) bash -c 'source scripts/nix-eval-helpers.sh && \
		eval_hm_simple "hm.config.home.activation.seedHyprShellRuntime.data" && \
		eval_hm_simple "hm.config.xdg.configFile.\"hypr/shell-profile.lua\".text" && \
		eval_hm_full "" "let sorted = flake.inputs.home-manager.lib.hm.dag.topoSort hm.config.home.activation; in if sorted ? result then \"ok\" else builtins.throw \"Home Manager activation DAG contains a cycle\"" && \
		eval_hm_full "" "builtins.toString (hm.config.xdg.configFile.\"hypr/end4\".source)" && \
		eval_hm_full "" "if builtins.hasAttr \"hypr/end4/keybinds.lua\" hm.config.xdg.configFile then builtins.throw \"end4 family must not install files beneath the managed hypr/end4 tree\" else \"ok\"" && \
		eval_hm_full "" "builtins.toString (hm.config.xdg.configFile.\"quickshell/end4-pC\".source)"'

.PHONY: nix-end4-hypr-build
nix-end4-hypr-build: ## Realize and inspect the validated Home Manager End4 Hypr artifact
	@artifact="$$(WAHRWELT_NIXOS_DIR='$(NIXOS_DIR)' nix build $(NIX_CACHE_ARGS) --no-link --no-write-lock-file --impure --print-out-paths --expr '$(END4_HYPR_EXPR)')"; \
	printf 'End4 Hypr artifact: %s\n' "$$artifact"; \
	cd $(REPO_ROOT) && bash Linux/dots/hypr/scripts/tests/end4-artifact-test.sh "$$artifact"

.PHONY: nix-end4-pc-quickshell-build
nix-end4-pc-quickshell-build: ## Realize and inspect the managed End4 pC QuickShell artifact
	@artifact="$$(WAHRWELT_NIXOS_DIR='$(NIXOS_DIR)' nix build $(NIX_CACHE_ARGS) --no-link --no-write-lock-file --impure --print-out-paths --expr '$(END4_PC_QUICKSHELL_EXPR)')"; \
	printf 'End4 pC QuickShell artifact: %s\n' "$$artifact"; \
	cd $(REPO_ROOT) && bash Linux/dots/hypr/scripts/tests/end4-artifact-test.sh --quickshell "$$artifact"

.PHONY: nix-shell-transition-build
nix-shell-transition-build: ## Realize and inspect the managed QuickShell transition artifact
	@artifact="$$(WAHRWELT_NIXOS_DIR='$(NIXOS_DIR)' nix build $(NIX_CACHE_ARGS) --no-link --no-write-lock-file --impure --print-out-paths --expr '$(SHELL_TRANSITION_EXPR)')"; \
	printf 'QuickShell transition artifact: %s\n' "$$artifact"; \
	cd $(REPO_ROOT) && $(NODE) Linux/dots/hypr/scripts/tests/shell-transition-artifact-test.mjs "$$artifact"

.PHONY: nix-profile-eval
nix-profile-eval: ## Eval Home Manager imports for every package preset
	@$(NIX_HM_ENV) bash -c 'source scripts/nix-eval-helpers.sh && \
		for preset in minimal desktop developer personal; do \
			echo "nix profile eval: $$preset"; \
			eval_hm_full "$$preset" "hm.config.home.username"; \
		done'

.PHONY: nix-hardening-contract
nix-hardening-contract: ## Check Nix security defaults and scoped v1_to_v2 migration behavior
	cd $(REPO_ROOT) && bash Linux/installer/scripts/nix-brand-migration-v1-to-v2-test.sh
	cd $(REPO_ROOT) && bash Linux/installer/scripts/nix-hardening-contract-test.sh

.PHONY: nix-installed-mirror-build
nix-installed-mirror-build: ## Build Wahrwelt and its supported compatibility alias from an /etc/nixos-style mirror
	@tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	rsync -a ../NixOS/ "$$tmp"/; \
	rsync -a ../dots/ "$$tmp"/dots/; \
	rsync -a --exclude=/bin/ --exclude=/tmp/ --exclude=/coverage.out --exclude=/coverage.html ./ "$$tmp"/installer/; \
	nix build $(NIX_CACHE_ARGS) --no-link --no-write-lock-file "path:$$tmp#wahrwelt"; \
	nix build $(NIX_CACHE_ARGS) --no-link --no-write-lock-file "path:$$tmp#mysetup"

.PHONY: check
check: ## Run the full local verification suite and remove build artefacts
	@status=0; \
	$(MAKE) fmt-check || status=$$?; \
	if [ $$status -eq 0 ]; then $(MAKE) lint || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) vet || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) shell-check || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) hypr-bind-check || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) hypr-integration-check || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) test || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) build || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) nix-package-eval || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) nix-hardening-contract || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) nix-profile-eval || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) nix-build || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) nix-noctalia-hm-compat || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) nix-hm-eval || status=$$?; fi; \
	if [ $$status -eq 0 ]; then $(MAKE) nix-installed-mirror-build || status=$$?; fi; \
	$(MAKE) clean || status=$$?; \
	exit $$status

##@ Help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN{FS=":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z0-9_-]+:.*?##/ {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5)}' $(MAKEFILE_LIST)
