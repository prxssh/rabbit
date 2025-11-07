package retry

import (
	"context"
	"fmt"
	"math"
	"time"
)

type Operation func(ctx context.Context) error

type Config struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	OnRetry      func(attempt int, err error, nextDelay time.Duration)
	RetryIf      func(err error) bool
}

type Option func(*Config)

func DefaultConfig() *Config {
	return &Config{
		MaxAttempts:  5,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
		OnRetry:      nil,
		RetryIf:      nil,
	}
}

func WithInitialDelay(delay time.Duration) Option {
	return func(c *Config) {
		c.InitialDelay = delay
	}
}

func WithMaxAttempts(maxAttempts int) Option {
	return func(c *Config) {
		c.MaxAttempts = maxAttempts
	}
}

func WithMaxDelay(delay time.Duration) Option {
	return func(c *Config) {
		c.MaxDelay = delay
	}
}

func WithMultiplier(multiplier float64) Option {
	return func(c *Config) {
		c.Multiplier = multiplier
	}
}

func WithOnRetry(callback func(attempt int, err error, nextDelay time.Duration)) Option {
	return func(c *Config) {
		c.OnRetry = callback
	}
}

func WithRetryIf(predicate func(err error) bool) Option {
	return func(c *Config) {
		c.RetryIf = predicate
	}
}

func Do(ctx context.Context, op Operation, opts ...Option) error {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context canceled before attempt %d: %w", attempt, err)
		}

		lastErr = op(ctx)
		if lastErr == nil {
			return nil
		}

		if cfg.RetryIf != nil && !cfg.RetryIf(lastErr) {
			return fmt.Errorf("unretryable error: %w", lastErr)
		}

		if attempt == cfg.MaxAttempts {
			break
		}

		delay := calculateDelay(attempt, cfg)

		if cfg.OnRetry != nil {
			cfg.OnRetry(attempt, lastErr, delay)
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf(
				"context canceled during retry wait (attempt %d): %w (last error: %v)",
				attempt,
				ctx.Err(),
				lastErr,
			)

		case <-timer.C:
			// continue
		}
	}

	return nil
}

func calculateDelay(attempt int, cfg *Config) time.Duration {
	delay := min(
		float64(cfg.MaxDelay),
		float64(cfg.InitialDelay)*math.Pow(cfg.Multiplier, float64(attempt-1)),
	)
	return time.Duration(delay)
}

func WithExponentialBackoff(maxAttempts int, initialDelay, maxDelay time.Duration) []Option {
	return []Option{
		WithMaxAttempts(maxAttempts),
		WithInitialDelay(initialDelay),
		WithMaxDelay(maxDelay),
		WithMultiplier(2.0),
	}
}

func WithLinearBackoff(maxAttempts int, delay time.Duration) []Option {
	return []Option{
		WithMaxAttempts(maxAttempts),
		WithInitialDelay(delay),
		WithMaxDelay(delay),
		WithMultiplier(1.0),
	}
}
