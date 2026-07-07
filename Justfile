# keel task runner — thin wrappers around keel-dev and go.

# Build keel-dev into ./bin.
build:
    mkdir -p bin
    go build -o bin/keel-dev ./cmd/keel-dev

# Run the verification gate (canonical: keel-dev ci).
ci:
    go run ./cmd/keel-dev ci

# Remove build artifacts.
clean:
    rm -rf bin
