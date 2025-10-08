package main

import (
	"context"
	"embed"
	"log/slog"
	"os"

	"github.com/prxssh/rabbit/pkg/torrent"
	"github.com/prxssh/rabbit/pkg/utils/logging"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	setupLogger()

	client, err := torrent.NewClient()
	if err != nil {
		slog.Error("client initialization failed", "error", err)
		os.Exit(1)
	}

	err = wails.Run(&options.App{
		Title:            "Rabbit - BitTorrent Client & Search Engine",
		Width:            1024,
		Height:           768,
		Fullscreen:       true,
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        func(ctx context.Context) { client.Startup(ctx) },
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		Bind:             []any{client},
	})
	if err != nil {
		slog.Error("failed to start wails", "error", err)
		os.Exit(1)
	}
}

func setupLogger() {
	opts := logging.DefaultOptions()
	opts.SlogOpts.Level = slog.LevelDebug
	opts.SlogOpts.AddSource = false

	h := logging.NewPrettyHandler(os.Stdout, &opts)
	l := slog.New(h)
	slog.SetDefault(l)
}
