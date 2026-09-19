// Command astar-sync refreshes operator-selected public source snapshots.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/BrandonFilth/Astar-MCP/internal/catalog"
	gh "github.com/BrandonFilth/Astar-MCP/internal/github"
	"github.com/BrandonFilth/Astar-MCP/internal/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	source := flag.String("source", "", "ecosystem, astar-docs, or aradia-docs")
	revision := flag.String("revision", "", "full astar-docs commit SHA")
	paths := flag.String("paths", "", "comma-separated documentation paths; replaces the selected corpus")
	flag.Parse()
	if *source != "ecosystem" && *source != "astar-docs" && *source != "aradia-docs" {
		return fmt.Errorf("select a supported source")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	store, err := storage.Open(ctx, os.Getenv("MCP_DATA_DIR"))
	if err != nil {
		return fmt.Errorf("cannot open MCP_DATA_DIR")
	}
	defer store.Close()
	switch *source {
	case "ecosystem":
		err = catalog.ImportProjects(ctx, catalog.HTTPClient(), store, catalog.EcosystemURL)
	case "aradia-docs":
		err = catalog.ImportAradiaGuide(ctx, catalog.HTTPClient(), store)
	case "astar-docs":
		client := gh.New(os.Getenv("GITHUB_MCP_BINARY"), os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"))
		defer client.Close()
		err = catalog.ImportDocuments(ctx, client, store, *revision, strings.Split(*paths, ","))
	}
	if err != nil {
		return fmt.Errorf("source import failed: %w", err)
	}
	fmt.Println("Source snapshot replaced successfully.")
	return nil
}
