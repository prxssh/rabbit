package scheduler

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/prxssh/rabbit/internal/piece"
	"github.com/prxssh/rabbit/pkg/availabilitybucket"
	"github.com/prxssh/rabbit/pkg/bitfield"
)

type Config struct {
	DownloadStrategy         DownloadStrategy
	RequestTimeout           time.Duration
	EndgameThreshold         int8
	EndgameDuplicatePerBlock int8
}

func WithDefaultConfig() *Config {
	return &Config{
		DownloadStrategy:         DownloadStrategySequential,
		RequestTimeout:           5 * time.Second,
		EndgameThreshold:         5, // 5% of pieces
		EndgameDuplicatePerBlock: 5,
	}
}

type peerState struct {
	inflightRequests    int32
	maxInflightRequests int32
	addr                netip.AddrPort
	choking             bool
	work                chan Event
	pieces              bitfield.Bitfield
	blockAssignments    map[uint64]struct{}
}

func blockKey(pieceIdx, begin uint32) uint64 {
	return uint64(pieceIdx)<<32 | uint64(begin)
}

type PieceResult struct {
	PieceIdx int32
	Success  bool
}

type BlockData struct {
	PieceIdx uint32
	Begin    uint32
	PieceLen uint32
	Data     []byte
}

type Scheduler struct {
	cfg    *Config
	logger *slog.Logger

	mut                   sync.RWMutex
	downloadedPieces      bitfield.Bitfield
	endgameStarted        bool
	inflightPieceRequests int32

	peerMut sync.RWMutex
	peers   map[netip.AddrPort]*peerState

	pieceAvailabilityBucket *availabilitybucket.Bucket
	pieceManager            *piece.Manager

	peerEvent   chan Event
	outBlocks   chan<- *BlockData
	pieceResult <-chan *PieceResult
}

type Opts struct {
	Logger   *slog.Logger
	Config   *Config
	MaxPeers int32
}

func NewScheduler(
	pieceManager *piece.Manager,
	outBlocksQueue chan<- *BlockData,
	pieceResultQueue <-chan *PieceResult,
	opts *Opts,
) *Scheduler {
	if opts.Config == nil {
		opts.Config = WithDefaultConfig()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	n := int(pieceManager.PieceCount())
	maxAvail := int(opts.MaxPeers)

	return &Scheduler{
		cfg:                     opts.Config,
		logger:                  opts.Logger.With("component", "scheduler"),
		peers:                   make(map[netip.AddrPort]*peerState),
		downloadedPieces:        bitfield.New(n),
		endgameStarted:          false,
		inflightPieceRequests:   0,
		pieceAvailabilityBucket: availabilitybucket.NewBucket(n, maxAvail),
		peerEvent:               make(chan Event, 1000),
		pieceManager:            pieceManager,
		outBlocks:               outBlocksQueue,
		pieceResult:             pieceResultQueue,
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup

	wg.Go(func() { s.listenPeerEvent(ctx) })
	wg.Go(func() { s.listenVerifiedPieces(ctx) })
	wg.Go(func() { s.assignPeerWork(ctx) })

	wg.Wait()
}

func (s *Scheduler) GetPeerEventQueue() chan<- Event {
	return s.peerEvent
}

func (s *Scheduler) GetPeerWorkQueue(addr netip.AddrPort) <-chan Event {
	s.peerMut.Lock()
	defer s.peerMut.Unlock()

	if peer, exists := s.peers[addr]; exists {
		return peer.work
	}

	peerState := &peerState{
		inflightRequests: 0,
		addr:             addr,
		choking:          true,
		work:             make(chan Event),
		pieces:           bitfield.New(int(s.pieceManager.PieceCount())),
		blockAssignments: make(map[uint64]struct{}),
	}
	s.peers[addr] = peerState

	return peerState.work
}

func (s *Scheduler) listenPeerEvent(ctx context.Context) {
	logger := s.logger.With("function", "peer event loop")
	logger.Debug("started")

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-s.peerEvent:
			if !ok {
				return
			}

			s.handlePeerEvent(event)
		}
	}
}

func (s *Scheduler) listenVerifiedPieces(ctx context.Context) {
	logger := s.logger.With("function", "listen verified pieces")
	logger.Debug("started")

	for {
		select {
		case <-ctx.Done():
			return

		case result, ok := <-s.pieceResult:
			if !ok {
				return
			}

			s.pieceManager.MarkPieceVerified(result.PieceIdx, result.Success)
			if result.Success {
				s.broadcastHave(result.PieceIdx)
			}
		}
	}
}

func (s *Scheduler) assignPeerWork(ctx context.Context) {
	logger := s.logger.With("function", "work assignment loop")
	logger.Debug("started")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			candidates := make([]netip.AddrPort, len(s.peers))

			s.peerMut.RLock()
			for addr, peer := range s.peers {
				if !peer.choking {
					candidates = append(candidates, addr)
				}
			}
			defer s.peerMut.RUnlock()
		}
	}
}

func (s *Scheduler) broadcastHave(pieceIdx int32) {
	s.peerMut.RLock()
	defer s.peerMut.RUnlock()

	for addr, peer := range s.peers {
		if peer.pieces.Has(int(pieceIdx)) {
			continue
		}

		select {
		case peer.work <- NewHaveEvent(addr, uint32(pieceIdx)):

		default:
			s.logger.Warn(
				"unable to send HAVE message; work queue full",
				"peer", addr,
				"piece", pieceIdx,
			)
		}
	}
}

func (s *Scheduler) updateAvailability(bf bitfield.Bitfield, delta int) {
	s.mut.Lock()
	ourBF := s.downloadedPieces.Clone()
	s.mut.Unlock()

	for i := 0; i < int(s.pieceManager.PieceCount()); i++ {
		if bf.Has(i) && !ourBF.Has(i) {
			s.pieceAvailabilityBucket.Move(i, delta)
		}
	}
}
