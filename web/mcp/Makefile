BINARY  := imapsync-mcp
LDFLAGS := -s -w

.PHONY: build dist test clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

# One binary per desktop platform, for handing to whoever runs the MCP client.
dist:
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-amd64 .
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 .
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-windows-amd64.exe .

test:
	go test ./...

clean:
	rm -rf dist $(BINARY)
