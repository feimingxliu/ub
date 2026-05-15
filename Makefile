.PHONY: build test vet fmt lint tidy clean run version

BIN := ub

build:
	go build -o $(BIN) ./cmd/ub

test:
	go test ./...

vet:
	go vet ./...

fmt:
	@if command -v gofumpt >/dev/null 2>&1; then \
		gofumpt -w . ; \
	else \
		echo "gofumpt not installed, falling back to gofmt" >&2 ; \
		gofmt -w . ; \
	fi

lint: vet
	@if command -v gofumpt >/dev/null 2>&1; then \
		out=$$(gofumpt -l .) ; \
	else \
		out=$$(gofmt -l .) ; \
	fi ; \
	if [ -n "$$out" ]; then \
		echo "unformatted files:" >&2 ; echo "$$out" >&2 ; exit 1 ; \
	fi

tidy:
	go mod tidy

run: build
	./$(BIN)

version: build
	./$(BIN) --version

clean:
	rm -f $(BIN)
