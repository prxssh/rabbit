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

	"github.com/prxssh/rabbit/internal/peer"
	"golang.org/x/sync/errgroup"
)

const baseDelay = 15 * time.Second

type Config struct {
	// NumWant is the maximutm number of peers to request the tracker.
	NumWant uint32

	// AnnounceInterval overrides tracker's suggested interval.
	// 0 uses tracker default.
	AnnounceInterval time.Duration

	// DefaultAnnounceInterval is used if the tracker provides no interval.
	DefaultAnnounceInterval time.Duration

	// MinAnnounceInterval enforces a minimum time between announces.
	MinAnnounceInterval time.Duration

	// MaxAnnounceBackoff caps exponential backoff for failed announces.
	MaxAnnounceBackoff time.Duration

	// MaxBackoffShift is the maximum exponential factor (2^n)
	// for backoff.
	MaxBackoffShift int

	// MaxConsecutiveFailures is the number of failures before
	// stopping the loop.
	MaxConsecutiveFailures int

	// Port is the TCP port this client listens on for incoming peer
	// connections.
	Port uint16
}

func WithDefaultConfig() *Config {
	return &Config{
		NumWant:                 50,
		AnnounceInterval:        0,
		DefaultAnnounceInterval: 15 * time.Minute,
		MinAnnounceInterval:     5 * time.Minute,
		MaxAnnounceBackoff:      30 * time.Minute,
		MaxBackoffShift:         5, // 2^5 = 32 * 15s = ~8m
		MaxConsecutiveFailures:  5,
		Port:                    6969,
	}
}

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
	port       uint16
	numWant    uint32
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
	TotalAnnounces      uint64    `json:"totalAnnounces"`
	SuccessfulAnnounces uint64    `json:"successfulAnnounces"`
	FailedAnnounces     uint64    `json:"failedAnnounces"`
	TotalPeersReceived  uint64    `json:"totalPeersReceived"`
	CurrentSeeders      int64     `json:"currentSeeders"`
	CurrentLeechers     int64     `json:"currentLeechers"`
	LastAnnounce        time.Time `json:"lastAnnounce"`
	LastSuccess         time.Time `json:"lastSuccess"`
}

type Tracker struct {
	cfg    *Config
	logger *slog.Logger

	tierMut sync.RWMutex
	tiers   [][]*url.URL

	trackerMut sync.Mutex
	trackers   map[string]TrackerProtocol

	stats         *Stats
	peerAddrQueue chan<- peer.PeerAddr
	getState      func() *AnnounceParams
}

type TrackerOpts struct {
	GetState      func() *AnnounceParams
	PeerAddrQueue chan<- peer.PeerAddr
	Logger        *slog.Logger
	Config        *Config
}

func NewTracker(announce string, announceList [][]string, opts *TrackerOpts) (*Tracker, error) {
	if opts.Config == nil {
		return nil, errors.New("tracker: config missing")
	}
	if opts.GetState == nil {
		return nil, errors.New("tracker: GetState hook missing")
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

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Tracker{
		cfg:           opts.Config,
		logger:        logger.With("source", "tracker"),
		tiers:         tiers,
		stats:         &Stats{},
		peerAddrQueue: opts.PeerAddrQueue,
		getState:      opts.GetState,
		trackers:      make(map[string]TrackerProtocol),
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

	params.numWant = t.cfg.NumWant
	params.port = t.cfg.Port

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

			if t.peerAddrQueue != nil && params.Event != EventStopped {
				for _, peerAddr := range resp.Peers {
					select {
					case t.peerAddrQueue <- peer.PeerAddr{Addr: peerAddr, Source: peer.PeerSourceTracker}:
					default:
						t.logger.Debug(
							"peer addr queue full; droppping peer",
						)
					}
				}
			}

			t.logger.Info("announce success",
				"tier", tierIdx,
				"url", u.String(),
				"peers", len(resp.Peers),
				"seeders", resp.Seeders,
				"leechers", resp.Leechers,
			)

			return resp, nil
		}

		t.logger.Warn("announce tier exhausted", "tier", tierIdx)
	}

	t.stats.FailedAnnounces.Add(1)
	if lastErr == nil {
		lastErr = errors.New("tracker: no trackers available to announce")
	}

	return nil, lastErr
}

func (t *Tracker) announceLoop(ctx context.Context) error {
	l := t.logger.With("component", "announce loop")
	l.Debug("started")

	consecutiveFailures := 0
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
			params := t.getState()
			params.Event = EventStopped
			_, _ = t.Announce(sctx, params)
			scancel()

			return nil

		case <-timer.C:
			if consecutiveFailures >= t.cfg.MaxConsecutiveFailures {
				return fmt.Errorf(
					"tracker: exceeded max %d consecutive failures",
					t.cfg.MaxConsecutiveFailures,
				)
			}

			var nextInterval time.Duration

			params := t.getState()
			resp, err := t.Announce(ctx, params)
			if err != nil {
				consecutiveFailures++
				l.Warn(
					"announce failed, backing off",
					"error",
					err,
					"failures",
					consecutiveFailures+1,
				)

				nextInterval = calculateBackoff(
					consecutiveFailures,
					t.cfg.MaxBackoffShift,
					t.cfg.MaxAnnounceBackoff,
				)
			} else {
				consecutiveFailures = 0
				nextInterval = getNextAnnounceInterval(resp, t.cfg.AnnounceInterval, t.cfg.MinAnnounceInterval, t.cfg.DefaultAnnounceInterval)

				l.Debug("announce success, next in", "interval", nextInterval)
			}

			timer.Reset(nextInterval)
		}
	}
}

func (t *Tracker) snapshotTier(at int) []*url.URL {
	t.tierMut.Lock()
	defer t.tierMut.Unlock()

	return append([]*url.URL(nil), t.tiers[at]...)
}

func (t *Tracker) promoteWithinTier(tierIdx, urlIdx int) {
	t.tierMut.Lock()
	defer t.tierMut.Unlock()

	tier := t.tiers[tierIdx]
	if urlIdx <= 0 || urlIdx >= len(tier) {
		return
	}

	u := tier[urlIdx]
	copy(tier[1:urlIdx+1], tier[0:urlIdx])
	tier[0] = u

	t.logger.Debug("promoted tracker within tier",
		"tier", tierIdx,
		"from", urlIdx,
		"url", u.String(),
	)
}

func (t *Tracker) getTracker(u *url.URL) (TrackerProtocol, error) {
	key := u.String()

	t.trackerMut.Lock()
	tr, ok := t.trackers[key]
	t.trackerMut.Unlock()
	if ok {
		return tr, nil
	}

	t.trackerMut.Lock()
	defer t.trackerMut.Unlock()

	log := t.logger.With("scheme", u.Scheme, "host", u.Host, "path", u.EscapedPath())

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
		err = fmt.Errorf("tracker: unsupported scheme %q", u.Scheme)
	}
	if err != nil {
		return nil, err
	}

	t.trackers[key] = tracker
	t.logger.Debug("new tracker client cached", "url", key)

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
		return nil, errors.New("tracker: no vald announce urls found")
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

func calculateBackoff(failures, maxShift int, maxAnnounceBackoff time.Duration) time.Duration {
	shift := min(maxShift, failures-1)
	delay := min(maxAnnounceBackoff, baseDelay*(1<<uint(shift)))

	// jitter: [0.75 * delay, 1.25 * delay]
	jitter := time.Duration(rand.Int63n(int64(delay) / 2))
	return delay - (delay / 4) + jitter
}

func getNextAnnounceInterval(
	resp *AnnounceResponse,
	userInterval, minInterval, defaultInterval time.Duration,
) time.Duration {
	interval := resp.Interval

	if interval > 0 {
		// use tracker interval
	} else if userInterval > 0 {
		// user provided interval
		interval = userInterval
	} else {
		// fallback to default
		interval = defaultInterval
	}

	if resp.MinInterval > interval {
		interval = resp.MinInterval
	}

	if interval < minInterval {
		interval = minInterval
	}

	return interval
}
