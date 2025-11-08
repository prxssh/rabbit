package tracker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/prxssh/rabbit/internal/bencode"
	"github.com/prxssh/rabbit/pkg/cast"
)

const maxTrackerResponseSize = 2 * 1024 * 1024 // 2MB

type HTTPTracker struct {
	baseURL   *url.URL
	client    *http.Client
	mut       sync.RWMutex
	trackerID string
	logger    *slog.Logger
}

func NewHTTPTracker(url *url.URL, logger *slog.Logger) (*HTTPTracker, error) {
	logger = logger.With("type", "http")

	t := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  false,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return &HTTPTracker{
		logger:  logger,
		baseURL: url,
		client:  &http.Client{Transport: t, Timeout: 30 * time.Second},
	}, nil
}

func (ht *HTTPTracker) Announce(
	ctx context.Context,
	params *AnnounceParams,
) (*AnnounceResponse, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		ht.buildAnnounceURL(params),
		nil,
	)
	if err != nil {
		return nil, err
	}

	resp, err := ht.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf(
			"tracker: announce returned non-ok status %d:%s",
			resp.StatusCode,
			string(body),
		)
	}

	r, err := parseAnnounceResponse(resp.Body)
	if err != nil {
		return nil, err
	}

	if r.TrackerID != "" {
		ht.mut.Lock()
		ht.trackerID = r.TrackerID
		ht.mut.Unlock()
	}

	return r, nil
}

func (ht *HTTPTracker) buildAnnounceURL(params *AnnounceParams) string {
	u := *ht.baseURL
	q := u.Query()

	q.Set("info_hash", string(params.InfoHash[:]))
	q.Set("peer_id", string(params.PeerID[:]))
	q.Set("port", strconv.Itoa(int(params.port)))
	q.Set("uploaded", strconv.FormatUint(params.Uploaded, 10))
	q.Set("downloaded", strconv.FormatUint(params.Downloaded, 10))
	q.Set("left", strconv.FormatUint(params.Left, 10))
	q.Set("compact", "1")

	if params.numWant > 0 {
		q.Set("numwant", strconv.Itoa(int(params.numWant)))
	}
	if params.Key != 0 {
		q.Set("key", strconv.FormatUint(uint64(params.Key), 10))
	}
	if params.Event != EventNone {
		q.Set("event", params.Event.String())
	}

	ht.mut.RLock()
	trackerID := ht.trackerID
	ht.mut.RUnlock()

	if trackerID != "" {
		q.Set("trackerid", ht.trackerID)
	}

	u.RawQuery = q.Encode()
	return u.String()
}

func parseAnnounceResponse(r io.Reader) (*AnnounceResponse, error) {
	lr := io.LimitReader(r, maxTrackerResponseSize)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}

	raw, err := bencode.Unmarshal(data)
	if err != nil {
		return nil, err
	}

	dict, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tracker: announce expected dict but got %T", raw)
	}

	if failure, ok := dict["failure reason"].(string); ok {
		return nil, fmt.Errorf("tracker: announce failure %s", failure)
	}
	if warning, ok := dict["warning reason"].(string); ok {
		return nil, fmt.Errorf("tracker: announce warning %s", warning)
	}

	interval, err := cast.ToInt(dict["interval"])
	if err != nil {
		return nil, fmt.Errorf("tracker: interval %w", err)
	}

	peers, err := parsePeers(dict)
	if err != nil {
		return nil, fmt.Errorf("tracker: invalid peers %w", err)
	}

	minInterval, _ := cast.ToInt(dict["min interval"])
	seeders, _ := cast.ToInt(dict["complete"])
	leechers, _ := cast.ToInt(dict["incomplete"])
	trackerID, _ := cast.ToString(dict["trackerid"])

	return &AnnounceResponse{
		TrackerID:   trackerID,
		Seeders:     seeders,
		Leechers:    leechers,
		Peers:       peers,
		Interval:    time.Duration(interval) * time.Second,
		MinInterval: time.Duration(minInterval) * time.Second,
	}, nil
}

func parsePeers(d map[string]any) ([]netip.AddrPort, error) {
	peersData, ok := d["peers"]
	if !ok {
		return nil, nil
	}

	return decodePeers(peersData, false)
}
