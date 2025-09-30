package main

import (
	"log/slog"
	"os"

	"github.com/prxssh/rabbit/pkg/utils/logging"
)

func main() {
	setupLogger()

	slog.Info("rabbit is up and running...")
}

func setupLogger() {
	h := logging.NewPrettyHandler(os.Stdout, nil)
	l := slog.New(h)
	slog.SetDefault(l)
}
