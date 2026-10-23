# The sandbox cannot write ~/Library/Caches/go-build and the module proxy is
# blocked there; both can be overridden from the environment.
export GOCACHE := env("GOCACHE", env("TMPDIR", "/tmp") / "gocache")
export GOPROXY := env("GOPROXY", "off")

# Build the binary next to the scripts.
build:
    go build -o mac-cleanup .

# Run all tests, including the target script tests in ./scripts.
test *args:
    go test {{ args }} ./...

# Run all tests with the race detector.
race:
    go test -race ./...

# Static checks for Go and the target scripts.
lint:
    gofmt -l . | (! grep .)
    go vet ./...
    shellcheck -s bash scripts/lib.sh scripts/targets/*.sh

# Everything before a commit.
check: lint race build

# Scan only: print the items, change nothing.
list: build
    ./mac-cleanup -list
