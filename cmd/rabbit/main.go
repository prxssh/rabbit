package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prxssh/rabbit/pkg/torrent"
	"github.com/prxssh/rabbit/pkg/utils/logging"
)

func main() {
	setupLogger()

	if len(os.Args) < 2 {
		slog.Info("Usage: ./rabbit <torrent path>")
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		slog.Error(
			"Invalid or missing file at path",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	torrent, err := torrent.New(data)
	if err != nil {
		slog.Error(
			"Failed to parse torrent",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	torrent.Start(context.Background())
}

func setupLogger() {
	opts := logging.DefaultOptions()
	opts.SlogOpts.Level = slog.LevelDebug
	opts.SlogOpts.AddSource = true

	h := logging.NewPrettyHandler(os.Stdout, &opts)
	l := slog.New(h)

	slog.SetDefault(l)
}
