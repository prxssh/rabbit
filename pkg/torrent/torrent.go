package torrent

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/peer"
	"github.com/prxssh/rabbit/pkg/piece"
	"github.com/prxssh/rabbit/pkg/storage"
	"github.com/prxssh/rabbit/pkg/tracker"
	"golang.org/x/sync/errgroup"
)

const (
	// backoffStart is the initial retry delay when tracker announces fail.
	backoffStart = 15 * time.Second

	// backoffMax is the maximum retry delay for failed tracker announces.
	backoffMax = 5 * time.Minute

	// defaultAnnounceInterval is used when the tracker doesn't specify an
	// interval.
	defaultAnnounceInterval = 30 * time.Minute
)

// Torrent represents a single BitTorrent download session.
//
// It coordinates the tracker announce loop, peer management, and piece
// selection for downloading a torrent. Call Run to start the download and Stop
// to gracefully terminate it.
type Torrent struct {
	// Size is the total byte size of the torrent content.
	Size int64

	// ClientID is this client's unique 20-byte peer ID.
	ClientID [sha1.Size]byte

	// Metainfo contains the parsed torrent metadata.
	Metainfo *Metainfo

	// Tracker handles communication with the torrent tracker.
	Tracker *tracker.Tracker

	// PeerManager coordinates all peer connections and downloads.
	PeerManager *peer.Manager

	// Internal lifecycle management
	cancel   context.CancelFunc
	stopOnce sync.Once
}

func New(data []byte) (*Torrent, error) {
	clientID, err := generatePeerID()
	if err != nil {
		return nil, err
	}

	metainfo, err := ParseMetainfo(data)
	if err != nil {
		return nil, err
	}
	size := metainfo.Size()

	tracker, err := tracker.NewTracker(
		metainfo.Announce,
		metainfo.AnnounceList,
	)
	if err != nil {
		return nil, err
	}

	storage, err := storage.OpenSingleFile(
		"./data/torrents/download.torrent",
		size,
	)
	if err != nil {
		return nil, err
	}

	picker := piece.NewPicker(
		metainfo.Info.PieceLength,
		metainfo.Size(),
		metainfo.Info.Pieces,
		nil,
	)

	peerManager := peer.NewManager(
		clientID,
		metainfo.Info.Hash,
		len(metainfo.Info.Pieces),
		metainfo.Info.PieceLength,
		size,
		picker,
		storage,
		nil,
	)

	return &Torrent{
		ClientID:    clientID,
		Metainfo:    metainfo,
		Tracker:     tracker,
		PeerManager: peerManager,
		Size:        size,
	}, nil
}

func (t *Torrent) Run(ctx context.Context) error {
	ctx, t.cancel = context.WithCancel(ctx)
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return t.announceLoop(ctx) })
	eg.Go(func() error { return t.PeerManager.Run(ctx) })

	err := eg.Wait()

	t.sendStoppedEvent()

	return err
}

func (t *Torrent) Stop() {
	t.stopOnce.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}
	})
}

func (t *Torrent) announceLoop(ctx context.Context) error {
	event := tracker.EventStarted
	nextDelay := time.Duration(0)
	backoff := backoffStart

	for {
		if nextDelay > 0 {
			timer := time.NewTimer(nextDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		params := t.buildAnnounceParams(event)
		resp, err := t.Tracker.Announce(ctx, params)
		if err != nil {
			nextDelay = backoff
			if backoff < backoffMax {
				backoff *= 2
				if backoff > backoffMax {
					backoff = backoffMax
				}
			}
			event = tracker.EventNone
			continue
		}

		t.PeerManager.AdmitPeers(resp.Peers)
		backoff = backoffStart

		interval := resp.Interval
		if interval == 0 {
			interval = defaultAnnounceInterval
		}
		if resp.MinInterval > 0 && resp.MinInterval > interval {
			interval = resp.MinInterval
		}
		nextDelay = interval

		event = tracker.EventNone
	}
}

func (t *Torrent) sendStoppedEvent() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := t.buildAnnounceParams(tracker.EventStopped)
	_, _ = t.Tracker.Announce(ctx, params)
}

func (t *Torrent) buildAnnounceParams(
	event tracker.Event,
) *tracker.AnnounceParams {
	stats := t.PeerManager.Stats()

	return &tracker.AnnounceParams{
		InfoHash:   t.Metainfo.Info.Hash,
		PeerID:     t.ClientID,
		Port:       6969,
		Uploaded:   uint64(stats.TotalUploaded),
		Downloaded: uint64(stats.TotalDownloaded),
		Left:       uint64(t.Size - stats.TotalUploaded),
		Event:      event,
	}
}

func generatePeerID() ([sha1.Size]byte, error) {
	var peerID [sha1.Size]byte

	prefix := []byte("-EC0001-")
	copy(peerID[:], prefix)

	if _, err := rand.Read(peerID[len(prefix):]); err != nil {
		return [sha1.Size]byte{}, err
	}

	return peerID, nil
}
