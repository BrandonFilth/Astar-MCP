// astar-mcp serves the implemented tools over standard input/output.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	gh "github.com/BrandonFilth/Astar-MCP/internal/github"
	"github.com/BrandonFilth/Astar-MCP/internal/server"
	"github.com/BrandonFilth/Astar-MCP/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	setup, cancel := context.WithTimeout(ctx, 10*time.Second)
	store, err := storage.Open(setup, os.Getenv("MCP_DATA_DIR"))
	cancel()
	if err != nil {
		return err
	}
	defer store.Close()
	github := gh.New(os.Getenv("GITHUB_MCP_BINARY"), os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"))
	defer github.Close()
	if err := server.New(github, store).Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		return fmt.Errorf("MCP session terminated")
	}
	return nil
}
