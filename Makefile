.PHONY: check probe-sources probe-github install-github build
check:
	go vet ./...
	go test -race -p 1 ./...

install-github:
	python3 scripts/install-github-mcp.py

# Explicit live source check; creates one ephemeral Aradia demo credential if needed.
probe-sources:
	python3 scripts/check-sources.py --demo

# Requires explicit provider credentials and binary path in the environment.
probe-github:
	mkdir -p .local/evidence
	go run ./cmd/github-probe > .local/evidence/github.json

build:
	mkdir -p bin
	go build -o bin/astar-mcp ./cmd/astar-mcp
