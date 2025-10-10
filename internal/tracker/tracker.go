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

	"github.com/prxssh/rabbit/internal/config"
	"golang.org/x/sync/errgroup"
)

const (
	maxBackoffShift        = 5
	maxConsecutiveFailures = 5
)

type AnnounceParams struct {
	InfoHash   [sha1.Size]byte
	PeerID     [sha1.Size]byte
	Uploaded   uint64
	Downloaded uint64
	Left       uint64
	Event      Event
	Key        uint32
	TrackerID  string
	IP         string
	NumWant    uint32
	Port       uint16
}

type AnnounceResponse struct {
	TrackerID   string
	Interval    time.Duration
	MinInterval time.Duration
	Leechers    int64
	Seeders     int64
	Peers       []netip.AddrPort
}

type Event uint32

const (
	EventNone Event = iota
	EventStarted
	EventStopped
	EventCompleted
)

func (e Event) String() string {
	switch e {
	case EventNone:
		return "none"
	case EventStarted:
		return "started"
	case EventCompleted:
		return "completed"
	default:
		return "stopped"
	}
}

type TrackerProtocol interface {
	Announce(ctx context.Context, params *AnnounceParams) (*AnnounceResponse, error)
}

type Stats struct {
	TotalAnnounces      atomic.Uint64
	SuccessfulAnnounces atomic.Uint64
	FailedAnnounces     atomic.Uint64
	LastAnnounce        atomic.Int64
	LastSuccess         atomic.Int64
	TotalPeersReceived  atomic.Uint64
	CurrentSeeders      atomic.Int64
	CurrentLeechers     atomic.Int64
}

type TrackerMetrics struct {
	TotalAnnounces      uint64
	SuccessfulAnnounces uint64
	FailedAnnounces     uint64
	TotalPeersReceived  uint64
	CurrentSeeders      int64
	CurrentLeechers     int64
	LastAnnounce        time.Time
	LastSuccess         time.Time
}

type Tracker struct {
	tiers             [][]*url.URL
	mu                sync.Mutex
	trackers          map[string]TrackerProtocol
	log               *slog.Logger
	stats             *Stats
	onAnnounceStart   func() *AnnounceParams
	onAnnounceSuccess func(addrs []netip.AddrPort)
}

type TrackerOpts struct {
	OnAnnounceStart   func() *AnnounceParams
	OnAnnounceSuccess func(addrs []netip.AddrPort)
	Log               *slog.Logger
}

func NewTracker(announce string, announceList [][]string, opts *TrackerOpts) (*Tracker, error) {
	if opts.OnAnnounceStart == nil {
		return nil, errors.New("OnAnnounceStart hook missing")
	}
	if opts.OnAnnounceSuccess == nil {
		return nil, errors.New("OnAnnounceSuccess hook missing")
	}

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

	log := opts.Log.With("component", "tracker", "tiers", len(tiers))

	return &Tracker{
		log:               log,
		tiers:             tiers,
		stats:             &Stats{},
		onAnnounceStart:   opts.OnAnnounceStart,
		onAnnounceSuccess: opts.OnAnnounceSuccess,
		trackers:          make(map[string]TrackerProtocol),
	}, nil
}

func (t *Tracker) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return t.announceLoop(gctx) })
	return g.Wait()
}

func (t *Tracker) Stats() TrackerMetrics {
	s := t.stats

	lastAnn := s.LastAnnounce.Load()
	lastSuc := s.LastSuccess.Load()

	var lastAnnT, lastSucT time.Time
	if lastAnn > 0 {
		lastAnnT = time.Unix(lastAnn, 0)
	}
	if lastSuc > 0 {
		lastSucT = time.Unix(lastSuc, 0)
	}

	return TrackerMetrics{
		TotalAnnounces:      s.TotalAnnounces.Load(),
		SuccessfulAnnounces: s.SuccessfulAnnounces.Load(),
		FailedAnnounces:     s.FailedAnnounces.Load(),
		TotalPeersReceived:  s.TotalPeersReceived.Load(),
		CurrentSeeders:      s.CurrentSeeders.Load(),
		CurrentLeechers:     s.CurrentLeechers.Load(),
		LastAnnounce:        lastAnnT,
		LastSuccess:         lastSucT,
	}
}

func (t *Tracker) Announce(ctx context.Context, params *AnnounceParams) (*AnnounceResponse, error) {
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

			t.log.Info("announce success",
				"tier", tierIdx,
				"url", u.String(),
				"peers", len(resp.Peers),
				"seeders", resp.Seeders,
				"leechers", resp.Leechers,
			)

			return resp, nil
		}

		t.log.Warn("announce tier exhausted", "tier", tierIdx)
	}

	t.stats.FailedAnnounces.Add(1)
	if lastErr == nil {
		lastErr = errors.New("tracker: all tiers exhausted")
	}

	return nil, lastErr
}

func (t *Tracker) announceLoop(ctx context.Context) error {
	l := t.log.With("component", "announce loop")
	l.Debug("started")

	consecutiveFailures := 0
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Warn("context done; exiting!", "error", ctx.Err())
			sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)

			params := t.onAnnounceStart()
			params.Event = EventStopped
			_, _ = t.Announce(sctx, params)

			scancel()
			return nil

		case <-ticker.C:
			if consecutiveFailures >= maxConsecutiveFailures {
				return errors.New("failed announce; exhausted all attempts")
			}

			resp, err := t.Announce(ctx, t.onAnnounceStart())
			if err != nil {
				consecutiveFailures++
				backoff := calculateBackoff(consecutiveFailures, maxBackoffShift)
				ticker.Reset(backoff)
				continue
			}

			t.onAnnounceSuccess(resp.Peers)

			consecutiveFailures = 0
			ticker.Reset(getNextAnnounceInterval(resp))
		}
	}
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

	t.log.Debug("announce promote",
		"tier", tierIdx,
		"from", urlIdx,
		"url", u.String(),
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

	log := t.log.With("scheme", u.Scheme, "host", u.Host, "path", u.EscapedPath())

	var (
		tracker TrackerProtocol
		err     error
	)

	switch u.Scheme {
	case "http", "https":
		tracker, err = NewHTTPTracker(u, log)
	case "udp":
		tracker, err = NewUDPTracker(u, log)
	default:
		err = fmt.Errorf("tracker: unsupported schema %q", u.Scheme)
	}

	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.trackers[key] = tracker
	t.mu.Unlock()

	t.log.Debug("tracker cached")

	return tracker, nil
}

func buildAnnounceURLs(announce string, announceList [][]string) ([][]*url.URL, error) {
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

func calculateBackoff(failures int, maxShift int) time.Duration {
	const baseDelay = 15 * time.Second

	shift := failures - 1
	if shift > maxShift {
		shift = maxShift
	}

	delay := baseDelay * (1 << uint(shift))

	if delay > config.Load().MaxAnnounceBackoff {
		delay = config.Load().MaxAnnounceBackoff
	}

	jitter := time.Duration(rand.Int63n(int64(delay) / 2))
	return delay - (delay / 4) + jitter
}

func getNextAnnounceInterval(resp *AnnounceResponse) time.Duration {
	interval := config.Load().AnnounceInterval
	if interval == 0 {
		interval = 2 * time.Minute
	}

	if resp.Interval > 0 {
		interval = resp.Interval
	}
	if resp.MinInterval > 0 && resp.MinInterval > interval {
		interval = resp.MinInterval
	}

	if config.Load().MinAnnounceInterval > 0 &&
		interval < config.Load().MinAnnounceInterval {
		interval = config.Load().MinAnnounceInterval
	}

	return interval
}
