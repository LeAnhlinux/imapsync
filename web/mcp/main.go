// Command imapsync-mcp is an MCP server for the imapsync web UI. It runs on the
// user's machine over stdio and drives a deployed instance through its HTTP
// API with an API key, so a model can list profiles, start and watch syncs,
// and read their logs.
//
// Configuration is by environment:
//
//	IMAPSYNC_URL               the site, e.g. https://sync.example.com
//	IMAPSYNC_API_KEY           an isk_ key; sync:read, sync:write, profiles:read
//	IMAPSYNC_MCP_ALLOW_DELETE  1 to allow delete1/delete2/expunge options
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

func newServer(api *Client, allowDelete bool) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "imapsync", Version: version}, &mcp.ServerOptions{
		Instructions: "Drives mailbox migrations on an imapsync web server. Typical flow: " +
			"list_profiles → optionally browse_folders → start_sync (dry_run first for a new pair) → " +
			"get_job with wait_seconds until it stops running → get_job_log to explain failures.",
	})
	register(s, &tools{api: api, allowDelete: allowDelete})
	return s
}

func main() {
	// stdout is the protocol channel; anything else written there corrupts it.
	log.SetOutput(os.Stderr)

	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Println("imapsync-mcp", version)
		return
	}

	base, key := os.Getenv("IMAPSYNC_URL"), os.Getenv("IMAPSYNC_API_KEY")
	if base == "" || key == "" {
		log.Fatal("imapsync-mcp: set IMAPSYNC_URL and IMAPSYNC_API_KEY")
	}
	if !strings.HasPrefix(key, "isk_") {
		log.Fatal("imapsync-mcp: IMAPSYNC_API_KEY should be an API key (isk_...), created under API Access")
	}
	allowDelete := os.Getenv("IMAPSYNC_MCP_ALLOW_DELETE") == "1"

	if err := newServer(NewClient(base, key), allowDelete).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
