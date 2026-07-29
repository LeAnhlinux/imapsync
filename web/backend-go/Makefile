BINARY  := imapsync-web
ADMIN   := createadmin
LDFLAGS := -s -w

.PHONY: build linux test clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/server
	go build -ldflags "$(LDFLAGS)" -o $(ADMIN) ./cmd/createadmin

# Static linux/amd64 binaries, cross-compiled from any host. The SQLite driver
# is pure Go, so CGO stays off and the result runs on any glibc or musl host.
linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY) ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(ADMIN) ./cmd/createadmin

test:
	go test ./...

clean:
	rm -rf dist $(BINARY) $(ADMIN)
