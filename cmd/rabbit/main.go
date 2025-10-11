package main

import (
	"context"
	"embed"
	"log/slog"
	"os"

	"github.com/prxssh/rabbit/internal/config"
	"github.com/prxssh/rabbit/internal/torrent"
	"github.com/prxssh/rabbit/internal/utils/logging"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	setupLogger()

	if err := config.Init(); err != nil {
		slog.Error("failed to initialize config", "error", err)
		os.Exit(1)
	}

	client := torrent.NewClient()

	err := wails.Run(&options.App{
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
	opts.SlogOpts.Level = slog.LevelInfo
	opts.SlogOpts.AddSource = false

	h := logging.NewPrettyHandler(os.Stdout, &opts)
	l := slog.New(h)
	slog.SetDefault(l)
}
