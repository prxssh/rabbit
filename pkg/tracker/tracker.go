package tracker

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AnnounceParams contains all information needed for a tracker announce.
type AnnounceParams struct {
	// InfoHash uniquely identifies the torrent (SHA-1 of info dict).
	InfoHash [sha1.Size]byte

	// PeerID uniquely identifies this client instance.
	PeerID [sha1.Size]byte

	// Uploaded counts total bytes uploaded to peers (cumulative).
	Uploaded uint64

	// Downloaded counts total bytes downloaded from peers (cumulative).
	Downloaded uint64

	// Left indicates remaining bytes to download (0 when complete).
	Left uint64

	// Event signals lifecycle transitions (started, stopped, completed).
	Event Event

	// Key is an optional randomized value for NAT traversal.
	Key uint32

	// TrackerID is an opaque token from previous response (HTTP trackers).
	TrackerID string

	// IP allows manual IP override (usually auto-detected by tracker).
	IP string

	// numWant requests a specific peer count. 0 uses tracker default.
	NumWant uint32

	// port is the TCP port this client listens on for incoming connections.
	Port uint16
}

// AnnounceResponse contains peer list and swarm statistics from tracker.
type AnnounceResponse struct {
	// TrackerID is an opaque token to include in next announce (HTTP only).
	TrackerID string

	// Interval specifies when to send next regular announce.
	Interval time.Duration

	// MinInterval is the minimum allowed time between announces.
	MinInterval time.Duration

	// Leechers counts incomplete downloaders in the swarm.
	Leechers int64

	// Seeders counts complete uploaders in the swarm.
	Seeders int64

	// Peers contains connectable peer addresses (IPv4 and/or IPv6).
	Peers []netip.AddrPort
}

// Event represents lifecycle states communicated to tracker.
type Event uint32

const (
	// EventNone is used for regular periodic announces.
	EventNone Event = iota

	// EventStarted signals the first announce after starting download.
	EventStarted

	// EventStopped signals graceful shutdown (last chance to update stats).
	EventStopped

	// EventCompleted signals download completion (transition to seeding).
	EventCompleted
)

func (e Event) String() string {
	switch e {
	case EventStarted:
		return "started"
	case EventStopped:
		return "stopped"
	case EventCompleted:
		return "completed"
	default:
		return ""
	}
}

const (
	strideV4 = 6
	strideV6 = 18
)

// TrackerProtocol abstracts HTTP, UDP, and potential future protocols.
type TrackerProtocol interface {
	// Announce performs a single announce request with timeout and returns
	// peer list or error.
	Announce(
		ctx context.Context,
		params *AnnounceParams,
	) (*AnnounceResponse, error)
}

// Stats provides runtime metrics about tracker operation.
type Stats struct {
	// TotalAnnounces counts all announce attempts (success + failure).
	TotalAnnounces atomic.Uint64

	// SuccessfulAnnounces counts successful responses.
	SuccessfulAnnounces atomic.Uint64

	// FailedAnnounces counts all failures across all tiers.
	FailedAnnounces atomic.Uint64

	// LastAnnounce records the timestamp of the most recent attempt.
	LastAnnounce atomic.Int64

	// LastSuccess records the timestamp of the most recent success.
	LastSuccess atomic.Int64

	// TotalPeersReceived accumulates peer count across all responses.
	TotalPeersReceived atomic.Uint64

	// CurrentSeeders holds the last reported seeder count.
	CurrentSeeders atomic.Int64

	// CurrentLeechers holds the last reported leecher count.
	CurrentLeechers atomic.Int64
}

// Tracker manages multi-tier tracker communication with failover, retries, and
// promotion strategies.
//
// Thread-safety: All methods are safe for concurrent use.
type Tracker struct {
	// tiers organizes announce URLs in preference order. Inner arrays are
	// tried in parallel (conceptually), outer arrays are fallback levels.
	tiers [][]*url.URL

	// mu protects tiers and trackers maps during promotion and lazy init.
	mu sync.Mutex

	// trackers caches protocol-specific clients keyed by URL string.
	trackers map[string]TrackerProtocol

	// log provides structured logging with component context.
	log *slog.Logger

	// stats exposes runtime metrics.
	stats Stats
}

// NewTracker constructs a tracker client from announce URL(s) and config.
//
// The announce parameter is the primary URL (from .torrent file).
// The announceList is an optional tier list (from announce-list extension).
//
// Returns error if no valid URLs can be parsed.
func NewTracker(
	announce string,
	announceList [][]string,
	log *slog.Logger,
) (*Tracker, error) {
	tiers, err := buildAnnounceURLs(announce, announceList)
	if err != nil {
		return nil, err
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := range tiers {
		if len(tiers[i]) < 2 {
			continue
		}

		r.Shuffle(len(tiers[i]), func(a, b int) {
			tiers[i][a], tiers[i][b] = tiers[i][b], tiers[i][a]
		})
	}

	log = slog.Default().With("component", "tracker", "tiers", len(tiers))

	return &Tracker{
		log:      log,
		tiers:    tiers,
		trackers: make(map[string]TrackerProtocol),
	}, nil
}

func (t *Tracker) Run(ctx context.Context) error {
	var err error

	go func() {
		t.stats.TotalAnnounces.Add(1)
		t.stats.LastAnnounce.Store(time.Now().Unix())
	}()

	return err
}

// Announce performs a single synchronous announce across all tiers with
// failover. It tries each tier in order until success or all tiers exhausted.
//
// Within a tier, trackers are tried sequentially (not truly parallel to avoid
// hammering multiple endpoints simultaneously).
//
// Returns the first successful response or the last error encountered.
func (t *Tracker) Announce(
	ctx context.Context,
	params *AnnounceParams,
) (*AnnounceResponse, error) {
	t.stats.TotalAnnounces.Add(1)
	t.stats.LastAnnounce.Store(time.Now().Unix())

	var lastErr error

	for tierIdx := 0; tierIdx < len(t.tiers); tierIdx++ {
		tier := t.snapshotTier(tierIdx)

		for i, u := range tier {
			tracker, err := t.getTracker(u)
			if err != nil {
				lastErr = err
				continue
			}

			resp, err := tracker.Announce(ctx, params)
			if err != nil {
				lastErr = err
				continue
			}

			t.promoteWithinTier(tierIdx, i)

			t.stats.SuccessfulAnnounces.Add(1)
			t.stats.LastSuccess.Store(time.Now().Unix())
			t.stats.TotalPeersReceived.Add(uint64(len(resp.Peers)))
			t.stats.CurrentSeeders.Store(resp.Seeders)
			t.stats.CurrentLeechers.Store(resp.Leechers)

			t.log.Info(
				"announce.success",
				"tier", tierIdx,
				"url", u.String(),
				"peers", len(resp.Peers),
				"seeders", resp.Seeders,
				"leechers", resp.Leechers,
			)

			return resp, nil
		}

		t.log.Warn("announce.tier.exhausted", "tier", tierIdx)
	}

	t.stats.FailedAnnounces.Add(1)
	if lastErr == nil {
		lastErr = errors.New("tracker: all tiers exhausted")
	}

	return nil, lastErr
}

// Stats returns a snapshot of current tracker statistics.
func (t *Tracker) Stats() *Stats {
	return &t.stats
}

func (t *Tracker) runAnnounceLoop(
	ctx context.Context,
	params *AnnounceParams,
) error {
	return nil
}

func (t *Tracker) snapshotTier(at int) []*url.URL {
	t.mu.Lock()
	defer t.mu.Unlock()

	return append([]*url.URL(nil), t.tiers[at]...)
}

func (t *Tracker) promoteWithinTier(tierIdx, urlIdx int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tier := t.tiers[tierIdx]
	if urlIdx <= 0 || urlIdx >= len(tier) {
		return
	}

	u := tier[urlIdx]
	copy(tier[1:urlIdx+1], tier[0:urlIdx])
	tier[0] = u

	t.log.Debug(
		"announce.promote",
		slog.Int("tier", tierIdx),
		slog.Int("from", urlIdx),
		slog.String("url", u.String()),
	)
}

func (t *Tracker) getTracker(u *url.URL) (TrackerProtocol, error) {
	key := u.String()

	t.mu.Lock()
	tr, ok := t.trackers[key]
	t.mu.Unlock()
	if ok {
		return tr, nil
	}

	ul := t.log.With(
		"scheme",
		u.Scheme,
		"host",
		u.Host,
		"path",
		u.EscapedPath(),
	)

	var (
		tracker TrackerProtocol
		err     error
	)

	switch u.Scheme {
	case "http", "https":
		tracker, err = NewHTTPTracker(
			u,
			ul.With("component", "tracker.http"),
		)
	case "udp":
		tracker, err = NewUDPTracker(
			u,
			ul.With("component", "tracker.udp"),
		)
	default:
		err = fmt.Errorf("tracker: unsupported schema %q", u.Scheme)
	}

	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.trackers[key] = tracker
	t.mu.Unlock()

	t.log.Debug("tracker.cached")

	return tracker, nil
}

func buildAnnounceURLs(
	announce string,
	announceList [][]string,
) ([][]*url.URL, error) {
	tiers := make([][]*url.URL, 0, len(announceList))

	if s := strings.TrimSpace(announce); s != "" {
		if u, ok := parseTrackerURL(s); ok {
			tiers = append(tiers, []*url.URL{u})
		}
	}

	for _, tier := range announceList {
		out := make([]*url.URL, 0, len(tier))

		for _, str := range tier {
			if u, ok := parseTrackerURL(str); ok {
				out = append(out, u)
			}
		}

		if len(out) > 0 {
			tiers = append(tiers, out)
		}
	}

	if len(tiers) == 0 {
		return nil, errors.New("tracker: no announce urls")
	}
	return tiers, nil
}

func parseTrackerURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}

	switch u.Scheme {
	case "http", "https", "udp":
		return u, true
	default:
		return nil, false
	}
}
