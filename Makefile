BIN      := subtx-gen
PKG      := ./...
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.Version=$(VERSION)

.PHONY: all build test lint tidy clean install-source

all: build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/subtx-gen

test:
	go test -race -count=1 $(PKG)

lint:
	golangci-lint run

tidy:
	go mod tidy

clean:
	rm -f $(BIN)

# Push the binary into the `source` LXD VM for end-to-end lab tests.
install-source: build
	lxc file push $(BIN) source/usr/local/bin/$(BIN)
	lxc exec source -- chmod +x /usr/local/bin/$(BIN)
