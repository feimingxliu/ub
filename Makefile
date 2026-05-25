.PHONY: build install test vet fmt lint check schema tidy clean run version install-hooks ensure-gofumpt changelog release-notes release

EXE :=
ifeq ($(OS),Windows_NT)
EXE := .exe
endif
BIN := ub$(EXE)
GOFUMPT_VERSION ?= mvdan.cc/gofumpt@latest
VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
RELEASE_NOTES ?= .release-notes.md

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
	./scripts/release-notes.sh "$(VERSION)" CHANGELOG.md > $(RELEASE_NOTES)

# release VERSION=x.y.z — regenerate CHANGELOG.md including the new
# version section, commit it, tag it, and push commit + tag. The tag
# commit itself contains the changelog, so the release workflow only
# needs to extract release notes (no regeneration in CI).
release:
	@if [ -z "$(VERSION)" ]; then \
		echo "usage: make release VERSION=x.y.z" >&2 ; \
		exit 2 ; \
	fi
	@case "$(VERSION)" in \
		v*) echo "VERSION must not include leading v" >&2 ; exit 2 ;; \
		*[!0-9.]*|"") echo "VERSION must look like x.y.z" >&2 ; exit 2 ;; \
	esac
	@if ! git diff --quiet || ! git diff --cached --quiet; then \
		echo "working tree not clean — commit or stash first" >&2 ; \
		git status --short >&2 ; \
		exit 1 ; \
	fi
	@if git rev-parse "v$(VERSION)" >/dev/null 2>&1; then \
		echo "tag v$(VERSION) already exists" >&2 ; \
		exit 1 ; \
	fi
	@branch=$$(git symbolic-ref --short HEAD) ; \
	if [ "$$branch" != "main" ]; then \
		echo "refusing to release from branch '$$branch' (expected main)" >&2 ; \
		exit 1 ; \
	fi
	$(MAKE) check
	git cliff --tag "v$(VERSION)" --output CHANGELOG.md
	git add CHANGELOG.md
	git commit -m "chore(release): v$(VERSION)"
	git tag -a "v$(VERSION)" -m "Release v$(VERSION)"
	git push origin main "v$(VERSION)"

tidy:
	go mod tidy

run: build
	./$(BIN)

version: build
	./$(BIN) --version

clean:
	rm -f $(BIN)
