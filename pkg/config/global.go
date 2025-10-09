package config

import "sync/atomic"

var cfg atomic.Value

func Init() {
	dcfg := defaultConfig()
	c := dcfg
	cfg.Store(&c)
}

// Load returns the current config (treat as read-only).
func Load() *Config {
	return cfg.Load().(*Config)
}

// Update applies a mutation on a copy and swaps it atomically.
func Update(mut func(*Config)) *Config {
	curr := Load()
	next := *curr
	mut(&next)
	cfg.Store(&next)
	return &next
}

// Swap replaces the global config atomically with the provided value.
func Swap(next Config) *Config {
	cfg.Store(&next)
	return &next
}
