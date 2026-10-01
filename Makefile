
# Ammit build tooling.
#
# The Go toolchain is expected on PATH. When developing inside a sandbox that
# only permits writes under the repository, source hack/goenv.sh first, which
# points GOROOT/GOPATH/GOCACHE at in-repo directories.

GO      ?= go
BIN     ?= bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
LDFLAGS := -X main.Version=$(VERSION)

.PHONY: all build test vet fmt check clean install adapter patch-check demo

all: check build

## build: compile both binaries into bin/
build:
	@mkdir -p $(BIN)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN)/ammit ./cmd/ammit
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN)/ammitd ./cmd/ammitd

## test: run the full test suite
test:
	$(GO) test ./...

## vet: run the vet tool over every package
vet:
	$(GO) vet ./...

## fmt: format every package
fmt:
	gofmt -w pkg cmd

## check: fail if anything is unformatted, then vet and test
check:
	@out=$$(gofmt -l pkg cmd); 	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	$(GO) test ./...

## adapter: build the Anubis integration (a separate module)
adapter:
	cd adapters/anubis && $(GO) build ./...

## install: install the CLI and daemon to GOBIN
install:
	$(GO) install -ldflags '$(LDFLAGS)' ./cmd/ammit ./cmd/ammitd

## demo: regenerate the README terminal recording from a real session
demo: build
	@mkdir -p docs
	./demo/scrape.sh > demo/session.txt
	python3 demo/render-svg.py demo/session.txt docs/demo.svg

## clean: remove build output
clean:
	rm -rf $(BIN)

## patch-check: verify the Anubis integration patch still applies
patch-check:
	@test -n "$$ANUBIS_SRC" || { echo "set ANUBIS_SRC to an Anubis checkout"; exit 1; }
	cd $$ANUBIS_SRC && git apply --check $(CURDIR)/adapters/anubis/patch/ammit-tier2.patch && echo "patch applies"
