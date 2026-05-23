.PHONY: build install test vet fmt lint check schema tidy clean run version install-hooks ensure-gofumpt changelog release-notes

EXE :=
ifeq ($(OS),Windows_NT)
EXE := .exe
endif
BIN := ub$(EXE)
GOFUMPT_VERSION ?= mvdan.cc/gofumpt@latest
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')

build:
	go build -o $(BIN) ./cmd/ub

install: build
	cp $(BIN) ~/.local/bin/

test:
	go test ./...

vet:
	go vet ./...

# ensure-gofumpt installs gofumpt if it's not on PATH. CI uses gofumpt
# strictly, so falling back to gofmt locally would let drift slip through.
ensure-gofumpt:
	@command -v gofumpt >/dev/null 2>&1 || { \
		echo "installing $(GOFUMPT_VERSION)…" >&2 ; \
		go install $(GOFUMPT_VERSION) ; \
	}

fmt: ensure-gofumpt
	gofumpt -w .

lint: ensure-gofumpt vet
	@out=$$(gofumpt -l .) ; \
	if [ -n "$$out" ]; then \
		echo "unformatted files (run 'make fmt'):" >&2 ; \
		echo "$$out" >&2 ; \
		exit 1 ; \
	fi

# check mirrors what CI enforces — keep these two in lockstep with
# .github/workflows/ci.yaml so a clean local `make check` predicts a
# green CI run. CGO_ENABLED=1 is forced for the race-enabled test step
# because `-race` requires cgo; some local environments (e.g.
# cross-compile setups) default to CGO_ENABLED=0.
check: lint
	CGO_ENABLED=1 go test ./... -race -count=1
	go build ./...

# install-hooks points git at .githooks/ so the repo's pre-commit /
# pre-push scripts run automatically. Idempotent; safe to rerun.
install-hooks:
	git config core.hooksPath .githooks
	@echo "git hooks installed (.githooks/ now active)"

schema:
	go run ./cmd/gen-schema

changelog:
	git cliff --output CHANGELOG.md

release-notes:
	mkdir -p dist
	./scripts/release-notes.sh "$(VERSION)" CHANGELOG.md > dist/release-notes.md

tidy:
	go mod tidy

run: build
	./$(BIN)

version: build
	./$(BIN) --version

clean:
	rm -f $(BIN)
