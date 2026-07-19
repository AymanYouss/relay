// Command relay is the entrypoint for the Relay LLM gateway.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/AymanYouss/relay/internal/app"
)

func main() {
	configPath := flag.String("config", envOr("RELAY_CONFIG", "relay.yaml"), "path to the configuration file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("relay %s\n", app.Version)
		return
	}

	if err := app.Run(*configPath); err != nil {
		slog.Error("relay exited with error", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
