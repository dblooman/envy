package main

import (
	"log/slog"
	"os"

	"github.com/dblooman/envy/demo/internal/server"
)

func main() {
	if err := server.Run("service-b"); err != nil {
		slog.Error("demo failed", "error", err)
		os.Exit(1)
	}
}
