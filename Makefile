# skills CLI — build / install / test entrypoints.
#
# Default target is `install`: drops the `skills` binary into $(GOBIN)
# (or $(HOME)/go/bin if GOBIN is unset) so it lands on your $PATH.
#
# Usage:
#   make            # same as `make install`
#   make install    # go install ./  →  $(GOBIN)/skills
#   make build      # go build -o bin/skills .  (local, no $GOBIN needed)
#   make test       # go test ./...
#   make vet        # go vet ./...
#   make lint       # vet + gofmt diff check
#   make clean      # remove ./bin/

PKG      := github.com/bizshuk/skills
BINARY   := skills
BIN_DIR  := bin
GO       ?= go

# Default to $GOBIN if set, else $HOME/go/bin (the standard post-Go-1.8 path).
# Falling back to $(HOME)/go/bin keeps `make install` working even when
# the user has not exported GOBIN.
GOBIN    ?= $(shell go env GOBIN 2>/dev/null)
ifeq ($(GOBIN),)
GOBIN    := $(HOME)/go/bin
endif

# `make` with no args runs `install`. Listed AFTER all phony targets so
# Make's default-target rule (first target in the file) doesn't pick `help`.
.DEFAULT_GOAL := install

.PHONY: help install build test vet lint clean

help: ## show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## build and install the skills binary into $(GOBIN)
	@echo "==> installing $(BINARY) to $(GOBIN)"
	@mkdir -p "$(GOBIN)"
	GOBIN="$(GOBIN)" $(GO) install .
	@echo "==> done: $(GOBIN)/$(BINARY)"

build: ## build the binary to ./bin/skills (no $GOBIN required)
	@echo "==> building $(BINARY) to $(BIN_DIR)/"
	@mkdir -p "$(BIN_DIR)"
	$(GO) build -o "$(BIN_DIR)/$(BINARY)" .

test: ## run all tests
	$(GO) test ./...

vet: ## run go vet
	$(GO) vet ./...

lint: vet ## vet + check that all files are gofmt-clean
	@echo "==> gofmt check"
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "$$out"; exit 1; fi

clean: ## remove ./bin
	rm -rf "$(BIN_DIR)"