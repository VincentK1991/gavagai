.PHONY: build test lint clean

# build compiles every package (verification) and writes the gavagai binary.
# `go build -o <file>` rejects multiple matched packages, so the binary is
# built from the root main package only.
build:
	go build ./...
	go build -o bin/gavagai .

# test runs the full unit test suite.
test:
	go test ./...

# lint runs golangci-lint (govet, staticcheck, errcheck, unused, gofmt, ...).
lint:
	golangci-lint run ./...

# clean removes build artifacts.
clean:
	rm -rf bin
