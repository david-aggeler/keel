# keel task runner — thin wrappers around keel-dev and go.

# Build admitted cmd/ binaries into ./bin.
build:
    mkdir -p bin
    # DHF-REQ: keel/requirement-27
    go build -o bin/keel-dev ./cmd/keel-dev
    go build -o bin/keel-demo ./cmd/keel-demo

# Run the verification gate (canonical: keel-dev ci).
ci:
    go run ./cmd/keel-dev ci

# Remove build artifacts.
clean:
    rm -rf bin
