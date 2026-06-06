.PHONY: build test lint clean

# build compiles the gavagai binary.
build:
	go build -o bin/gavagai ./...

# test runs the full unit test suite.
test:
	go test ./...

# lint runs golangci-lint (govet, staticcheck, errcheck, unused, gofmt, ...).
lint:
	golangci-lint run ./...

# clean removes build artifacts.
clean:
	rm -rf bin
