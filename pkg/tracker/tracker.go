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
	"time"
)

type AnnounceParams struct {
	InfoHash   [sha1.Size]byte
	PeerID     [sha1.Size]byte
	Port       uint16
	Uploaded   uint64
	Downloaded uint64
	Left       uint64
	Event      Event
	NumWant    uint32
	Key        uint32
	TrackerID  string
}

type AnnounceResponse struct {
	TrackerID   string
	Interval    time.Duration
	MinInterval time.Duration
	Leechers    int64
	Seeders     int64
	Peers       []netip.AddrPort
}

type ScrapeParams struct {
	AnnounceURLs []string
	InfoHashes   [][sha1.Size]byte
}

type ScrapeResponse struct {
	Stats map[[sha1.Size]byte]ScrapeStats
}

type ScrapeStats struct {
	Seeders   uint32
	Leechers  uint32
	Completed uint32
	Name      string
}

type Event uint32

const (
	EventNone Event = iota
	EventStarted
	EventStopped
	EventCompleted
)

const (
	strideV4 = 6
	strideV6 = 18
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

type TrackerProtocol interface {
	Announce(
		ctx context.Context,
		params *AnnounceParams,
	) (*AnnounceResponse, error)
}

type Tracker struct {
	tiers    [][]*url.URL
	mut      sync.Mutex
	trackers map[string]TrackerProtocol
	log      *slog.Logger
}

func NewTracker(announce string, announceList [][]string) (*Tracker, error) {
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

	return &Tracker{
		tiers:    tiers,
		trackers: make(map[string]TrackerProtocol),
		log: slog.Default().
			With("component", "tracker", "tiers", len(tiers)),
	}, nil
}

func (t *Tracker) Announce(
	ctx context.Context,
	params *AnnounceParams,
) (*AnnounceResponse, error) {
	var lastErr error

	for ti := 0; ti < len(t.tiers); ti++ {
		tier := t.snapshotTier(ti)

		for i, u := range tier {
			tracker, err := t.getTracker(u)
			if err != nil {
				lastErr = err
				continue
			}

			resp, err := tracker.Announce(ctx, params)
			if err == nil {
				t.promoteWithinTier(ti, i)
				return resp, nil
			}
			lastErr = err
		}
	}

	return nil, lastErr
}

func (t *Tracker) snapshotTier(at int) []*url.URL {
	t.mut.Lock()
	defer t.mut.Unlock()

	return append([]*url.URL(nil), t.tiers[at]...)
}

func (t *Tracker) promoteWithinTier(tierIdx, urlIdx int) {
	t.mut.Lock()
	defer t.mut.Unlock()

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

	t.mut.Lock()
	tr, ok := t.trackers[key]
	t.mut.Unlock()
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

	t.mut.Lock()
	t.trackers[key] = tracker
	t.mut.Unlock()

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
