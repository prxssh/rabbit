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
	"golang.org/x/sync/errgroup"
)

const peerMinInflightRequests = 5

type Config struct {
	DownloadStrategy         DownloadStrategy
	EndgameThreshold         uint8
	EndgameDuplicatePerBlock uint8
}

func WithDefaultConfig() *Config {
	return &Config{
		DownloadStrategy:         DownloadStrategySequential,
		EndgameThreshold:         5, // 5% of pieces
		EndgameDuplicatePerBlock: 5,
	}
}

type peerState struct {
	inflightRequests    uint32
	maxInflightRequests uint32
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
	PieceIdx uint32
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
	MaxPeers uint8
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

func (s *Scheduler) Run(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return s.listenPeerEvent(gctx) })
	g.Go(func() error { return s.listenVerifiedPieces(gctx) })
	g.Go(func() error { return s.assignPeerWork(gctx) })

	return g.Wait()
}

func (s *Scheduler) UpdateConfig(newCfg *Config) {
	if newCfg == nil {
		return
	}

	s.mut.Lock()
	oldStrategy := s.cfg.DownloadStrategy
	s.cfg = newCfg
	newStrategy := s.cfg.DownloadStrategy
	s.mut.Unlock()

	// If switching to sequential strategy, reset sequential state
	if oldStrategy != DownloadStrategySequential && newStrategy == DownloadStrategySequential {
		s.pieceManager.ResetSequentialState()
	}
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
		inflightRequests:    0,
		addr:                addr,
		choking:             true,
		maxInflightRequests: 50,
		work:                make(chan Event),
		pieces:              bitfield.New(int(s.pieceManager.PieceCount())),
		blockAssignments:    make(map[uint64]struct{}),
	}
	s.peers[addr] = peerState

	return peerState.work
}

func (s *Scheduler) listenPeerEvent(ctx context.Context) error {
	logger := s.logger.With("function", "peer event loop")
	logger.Debug("started")

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-s.peerEvent:
			if !ok {
				return nil
			}

			s.handlePeerEvent(event)
		}
	}
}

func (s *Scheduler) listenVerifiedPieces(ctx context.Context) error {
	logger := s.logger.With("function", "listen verified pieces")
	logger.Debug("started")

	for {
		select {
		case <-ctx.Done():
			return nil

		case result, ok := <-s.pieceResult:
			logger.Debug(
				"received verified piece",
				"piece",
				result.PieceIdx,
				"successful",
				result.Success,
			)
			if !ok {
				return nil
			}

			s.pieceManager.MarkPieceVerified(result.PieceIdx, result.Success)
			if result.Success {
				s.broadcastHave(result.PieceIdx)
			}
		}
	}
}

func (s *Scheduler) assignPeerWork(ctx context.Context) error {
	logger := s.logger.With("source", "work assignment loop")
	logger.Debug("started")

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-ticker.C:
			candidates := make([]netip.AddrPort, 0, len(s.peers))

			s.peerMut.RLock()
			for addr, peer := range s.peers {
				if !peer.choking {
					candidates = append(candidates, addr)
				}
			}
			s.peerMut.RUnlock()

			for _, candidatePeer := range candidates {
				s.nextForPeer(candidatePeer)
			}
		}
	}
}

func (s *Scheduler) broadcastHave(pieceIdx uint32) {
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

func (s *Scheduler) assignBlockToPeer(peer *peerState, block *piece.BlockInfo) {
	s.mut.Lock()
	s.inflightPieceRequests++
	s.mut.Unlock()

	key := blockKey(block.PieceIdx, block.Begin)

	s.peerMut.Lock()
	peer.blockAssignments[key] = struct{}{}
	s.peerMut.Unlock()

	select {
	case peer.work <- NewRequestEvent(peer.addr, block.PieceIdx, block.Begin, block.Length):

	default:
		s.logger.Warn("peer work queue full; dropping request", "peer", peer.addr)

		s.mut.Lock()
		s.inflightPieceRequests--
		s.mut.Unlock()

		s.peerMut.Lock()
		delete(peer.blockAssignments, key)
		s.peerMut.Unlock()

		s.pieceManager.UnassignBlock(peer.addr, block.PieceIdx, block.Begin)
	}
}
