# keel task runner — thin wrappers around keel-dev and go.

# Build admitted cmd/ binaries and local release artifacts into ./bin.
build:
    mkdir -p bin
    # DHF-REQ: keel/requirement-27
    go build -o bin/keel-dev ./cmd/keel-dev
    go build -o bin/keel-demo ./cmd/keel-demo
    # DHF-REQ: keel/requirement-45
    pnpm --dir vsix run package:vsix

# Publish a keel release through the canonical release verb.
publish version:
    go run ./cmd/keel-dev release {{version}}

# Run the verification gate (canonical: keel-dev ci).
ci:
    go run ./cmd/keel-dev ci

# Remove build artifacts.
clean:
    rm -rf bin
