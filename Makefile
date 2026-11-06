GO ?= go

.PHONY: build test vet dist clean

build:
	CGO_ENABLED=0 $(GO) build -o golinks .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -o dist/golinks-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -o dist/golinks-darwin-amd64 .

clean:
	rm -f golinks
	rm -rf dist
