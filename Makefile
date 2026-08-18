BINARY := npmctl
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test test-race lint fmt install clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/npmctl

test:
	go test ./...

# The race detector needs cgo, so it cannot share the static build's flags.
test-race:
	go test -race ./...

lint:
	go vet ./...
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "gofmt found unformatted files" && exit 1)

fmt:
	gofmt -w .

install:
	CGO_ENABLED=0 go install -ldflags "$(LDFLAGS)" ./cmd/npmctl

clean:
	rm -f $(BINARY)
	rm -rf dist
