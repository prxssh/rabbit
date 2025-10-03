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

type Tracker interface {
	Announce(
		ctx context.Context,
		params *AnnounceParams,
	) (*AnnounceResponse, error)
}

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

func NewTracker(announce string, announceList [][]string) (Tracker, error) {
	urls, err := buildAnnounceURLs(announce, announceList)
	if err != nil {
		return nil, err
	}

	return newMultiTierTracker(urls)
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

type multiTierTracker struct {
	tiers    [][]*url.URL
	mut      sync.Mutex
	trackers map[string]Tracker
	log      *slog.Logger
}

func newMultiTierTracker(tiers [][]*url.URL) (*multiTierTracker, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := range tiers {
		if len(tiers[i]) < 2 {
			continue
		}

		r.Shuffle(len(tiers[i]), func(a, b int) {
			tiers[i][a], tiers[i][b] = tiers[i][b], tiers[i][a]
		})
	}

	return &multiTierTracker{
		tiers:    tiers,
		trackers: make(map[string]Tracker),
		log: slog.Default().
			With("component", "tracker", "tiers", len(tiers)),
	}, nil
}

func (mt *multiTierTracker) Announce(
	ctx context.Context,
	params *AnnounceParams,
) (*AnnounceResponse, error) {
	var lastErr error

	for t := 0; t < len(mt.tiers); t++ {
		tier := mt.snapshotTier(t)

		for i, u := range tier {
			tracker, err := mt.getTracker(u)
			if err != nil {
				lastErr = err
				continue
			}

			resp, err := tracker.Announce(ctx, params)
			if err == nil {
				mt.promoteWithinTier(t, i)
				return resp, nil
			}
			lastErr = err
		}
	}

	return nil, lastErr
}

func (mt *multiTierTracker) snapshotTier(t int) []*url.URL {
	mt.mut.Lock()
	defer mt.mut.Unlock()

	return append([]*url.URL(nil), mt.tiers[t]...)
}

func (mt *multiTierTracker) promoteWithinTier(tierIdx, urlIdx int) {
	mt.mut.Lock()
	defer mt.mut.Unlock()

	tier := mt.tiers[tierIdx]
	if urlIdx <= 0 || urlIdx >= len(tier) {
		return
	}

	u := tier[urlIdx]
	copy(tier[1:urlIdx+1], tier[0:urlIdx])
	tier[0] = u

	mt.log.Debug(
		"announce.promote",
		slog.Int("tier", tierIdx),
		slog.Int("from", urlIdx),
		slog.String("url", u.String()),
	)
}

func (mt *multiTierTracker) getTracker(u *url.URL) (Tracker, error) {
	key := u.String()

	mt.mut.Lock()
	tr, ok := mt.trackers[key]
	mt.mut.Unlock()
	if ok {
		return tr, nil
	}

	ul := mt.log.With(
		"scheme",
		u.Scheme,
		"host",
		u.Host,
		"path",
		u.EscapedPath(),
	)

	var (
		tracker Tracker
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

	mt.mut.Lock()
	mt.trackers[key] = tracker
	mt.mut.Unlock()

	mt.log.Debug("tracker.cached")

	return tracker, nil
}
