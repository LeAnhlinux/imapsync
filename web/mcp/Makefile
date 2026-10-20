BINARY  := imapsync-mcp
LDFLAGS := -s -w

.PHONY: build dist downloads test clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

# One binary per desktop platform, for handing to whoever runs the MCP client.
dist:
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 .
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 .
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe .

# dist plus the manifest.json the web UI lists them from. deploy.sh copies the
# result into the frontend, where it is served under /downloads/mcp/.
VERSION := $(shell sed -n 's/^const version = "\(.*\)"/\1/p' main.go)
downloads: dist
	cp HUONG_DAN.md dist/
	./manifest.sh dist $(VERSION)

test:
	go test ./...

clean:
	rm -rf dist $(BINARY)
