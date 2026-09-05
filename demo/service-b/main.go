package main

import (
	"github.com/dblooman/envy/demo/internal/server"
	"log/slog"
	"os"
)

func main() {
	if err := server.Run("service-b"); err != nil {
		slog.Error("demo failed", "error", err)
		os.Exit(1)
	}
}
