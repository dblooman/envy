package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dblooman/envy/internal/client"
	"github.com/dblooman/envy/internal/loginclient"
	"github.com/dblooman/envy/internal/mcp"
	"net/http"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && ctx.Err() == nil {
		slog.Error("MCP server stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	baseURL := os.Getenv("ENVY_API_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8081"
	}
	token := os.Getenv("ENVY_API_TOKEN")
	if path := os.Getenv("ENVY_API_TOKEN_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read API token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	var hc *http.Client
	if token == "" && os.Getenv("ENVY_API_TOKEN_FILE") != "" {
		return fmt.Errorf("API token file is empty")
	}
	if token == "" {
		m, err := loginclient.New(baseURL)
		if err != nil {
			return err
		}
		hc = m.HTTPClient()
		baseURL = m.Base
	}
	c, err := client.NewWithIdentity(baseURL, token, hc, "mcp", os.Getenv("ENVY_TASK_ID"))
	if err != nil {
		return err
	}
	return mcp.Run(ctx, c)
}
