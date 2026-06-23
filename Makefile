.PHONY: all build build-server build-inspect build-genblocks \
        run run-server run-inspect \
        test test-all test-player test-world test-protocol test-config test-inspect test-genblocks \
        inspect-setup vet fmt clean tidy

# ── Default target ──
all: build

# ── Build ──
# `build` builds all three binaries into the project root.
# Each binary can be built individually via the build-* targets.
build: build-server build-inspect build-genblocks

build-server:
	go build -o server ./cmd/server/

build-inspect:
	go build -o goore-inspect ./cmd/inspect/

build-genblocks:
	go build -o genblocks ./cmd/genblocks/

# ── Run ──
# `run` runs the Minecraft server with the bundled test_world.
# `run-inspect` opens the TUI inspector against the same world.
# `run-server` is an alias for `run`.
run: run-server

run-server: build-server
	./server -world ./test_world -gamemode 0

run-inspect: build-inspect
	./goore-inspect ./test_world

# ── Test ──
# `test` (alias for `test-all`) runs every test in the project.
# Per-package targets are useful for fast iteration during TDD.
test: test-all

test-all:
	go test ./... -count=1

test-player:
	go test ./internal/player/ -v -count=1 -timeout 30s

test-world:
	go test ./internal/world/ -v -count=1

test-protocol:
	go test ./internal/protocol/... -v -count=1

test-config:
	go test ./internal/config/ -v -count=1

test-inspect:
	go test ./internal/inspect/ -v -count=1

test-genblocks:
	go test ./cmd/genblocks/ -v -count=1

# ── Inspect setup ──
# Populates ./test_world with a valid world.meta + 2 player files
# + 1 chunk file for manual testing of the TUI inspector. Idempotent.
inspect-setup:
	go test -run TestMain_SetupWorld -v ./internal/inspect/

# ── Quality ──
vet:
	go vet ./...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

# ── Clean ──
clean:
	rm -f server goore-inspect genblocks
	go clean -cache -testcache
