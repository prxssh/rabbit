package torrent

import (
	"github.com/prxssh/rabbit/internal/peer"
	"github.com/prxssh/rabbit/internal/scheduler"
	"github.com/prxssh/rabbit/internal/storage"
	"github.com/prxssh/rabbit/internal/tracker"
)

type Config struct {
	Scheduler *scheduler.Config
	Storage   *storage.Config
	Peer      *peer.Config
	Tracker   *tracker.Config
}

func WithDefaultConfig() *Config {
	return &Config{
		Scheduler: scheduler.WithDefaultConfig(),
		Storage:   storage.WithDefaultConfig(),
		Peer:      peer.WithDefaultConfig(),
		Tracker:   tracker.WithDefaultConfig(),
	}
}
