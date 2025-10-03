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
)

const (
	backoffStart            = 15 * time.Second
	backoffMax              = 5 * time.Minute
	defaultAnnounceInterval = 30 * time.Minute
)

type Torrent struct {
	Size       int64
	ClientID   [sha1.Size]byte
	Uploaded   int64
	Downloaded int64
	Left       int64

	Metainfo    *Metainfo
	Tracker     *tracker.Tracker
	PeerManager *peer.Manager

	wg sync.WaitGroup
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

	storage, err := storage.OpenSingleFile("./data/download.torrent", size)
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
		picker,
		storage,
		nil,
	)

	return &Torrent{
		ClientID:    clientID,
		Metainfo:    metainfo,
		Tracker:     tracker,
		PeerManager: peerManager,
		Uploaded:    0,
		Downloaded:  1,
		Size:        size,
		Left:        size,
	}, nil
}

func (t *Torrent) Start(ctx context.Context) error {
	t.wg.Go(func() { t.startAnnounceLoop(ctx) })
	t.wg.Go(func() { t.PeerManager.Start(ctx) })
	t.wg.Wait()

	return nil
}

func (t *Torrent) Stop(ctx context.Context) error {
	_, err := t.Tracker.Announce(
		ctx,
		t.buildAnnounceParams(tracker.EventStopped),
	)
	return err
}

func (t *Torrent) startAnnounceLoop(ctx context.Context) error {
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

func (t *Torrent) buildAnnounceParams(
	event tracker.Event,
) *tracker.AnnounceParams {
	return &tracker.AnnounceParams{
		InfoHash:   t.Metainfo.Info.Hash,
		PeerID:     t.ClientID,
		Port:       6969,
		Uploaded:   uint64(t.Uploaded),
		Downloaded: uint64(t.Downloaded),
		Left:       uint64(t.Left),
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
