package scheduler

import (
	"context"
	"crypto/sha1"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/bitfield"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Config struct {
	DownloadDir string

	// DownloadStrategy chooses how to rank eligible pieces.
	DownloadStrategy DownloadStrategy

	// MaxInflightRequestsPerPeer limits how many requests can be outstanding
	// to a single peer at once.
	MaxInflightRequestsPerPeer int

	// MinInflightRequestsPerPeer is a soft floor so slow/latent peers still
	// make progress (1–4 is typical). The controller will never drop below
	// this.
	MinInflightRequestsPerPeer int

	// RequestQueueTime is the target amount of data (in seconds) to keep
	// pipelined per peer (libtorrent: request_queue_time). The controller
	// sizes the per-peer window ≈ ceil((peer_rate * RTT * RequestQueueTime)/block_size),
	// clamped to [MinInflightRequestsPerPeer, MaxInflightRequestsPerPeer].
	RequestQueueTimeout time.Duration

	// RequestTimeout is the baseline time after which an in-flight block
	// can be considered timed-out and re-assigned. You can adapt it
	// per-peer using RTT.
	RequestTimeout time.Duration

	// EndgameDuplicatePerBlock, when Endgame is enabled, caps the number of
	// duplicate owners (peers concurrently fetching the same block).
	EndgameDuplicatePerBlock int

	// EndgameThreshold decides when to enter endgame based on remaining blocks.
	EndgameThreshold int

	// maxRequestBacklog is the maximum requests that the per-peer work queue
	// can have.
	maxRequestBacklog int
}

func WithDefaultConfig() *Config {
	return &Config{
		DownloadDir:                getDefaultDownloadDir(),
		MaxInflightRequestsPerPeer: 32,
		MinInflightRequestsPerPeer: 4,
		RequestQueueTimeout:        3 * time.Second,
		RequestTimeout:             25 * time.Second,
		EndgameDuplicatePerBlock:   5,
		EndgameThreshold:           30,
	}
}

func getDefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		if cwd, err := os.Getwd(); err == nil {
			return filepath.Join(cwd, "downloads")
		}
		return "./downloads"
	}

	switch runtime.Environment(context.Background()).Platform {
	case "windows":
		return filepath.Join(home, "Downloads", "rabbit")
	case "darwin":
		return filepath.Join(home, "Downloads", "rabbit")
	default: // linux, bsd, etc.
		return filepath.Join(home, ".local", "share", "rabbit", "downloads")
	}
}

type peerState struct {
	inflight         int
	choked           bool
	workQueue        chan *PieceRequest
	addr             netip.AddrPort
	bitfield         bitfield.Bitfield
	blockAssignments map[uint64]struct{}
}

func newPeerState(addr netip.AddrPort, pieceCount, workQueueSize int) *peerState {
	return &peerState{
		addr:             addr,
		bitfield:         bitfield.New(pieceCount),
		blockAssignments: make(map[uint64]struct{}),
		workQueue:        make(chan *PieceRequest, workQueueSize),
	}
}

// PieceScheduler is the central coordinator for a torrent download. It manages
// the state of all pieces, tracks peer availability, and implements the
// piece-picking strategy (e.g., rarest-first, sequential).
//
// All its methods that modify state are expected to be called from a single
// "event loop" goroutine, making most fields safe to access without locks
// *within* that loop. The eventQueue is the entry point for all state changes.
type PieceScheduler struct {
	log *slog.Logger
	cfg *Config

	mut sync.RWMutex
	// lastPieceLen is the byte length of the final piece (which may be shorter).
	lastPieceLen int32

	// pieceCount is the total number of pieces in the torrent.
	pieceCount int

	// pieces holds the detailed state for every piece, indexed by piece number.
	pieces []*piece

	// availability tracks piece rarity for the rarest-first algorithm.
	availability *availabilityBucket

	// nextPiece is the index of the next piece to pick for sequential download
	// (e.g., for streaming or to prioritize the start of the file).
	nextPiece int

	// nextBlock is the index of the next block within nextPiece to pick.
	nextBlock int

	// endgame is true when the download is in endgame mode (requesting all
	// remaining blocks from all available peers).
	endgame bool

	// remainingBlocks is a count of all blocks that are still in blockWant
	// state. This is often used to trigger endgame mode.
	remainingBlocks int

	// bitfield is our local bitfield, tracking which pieces we have verified.
	bitfield bitfield.Bitfield

	// inflightRequests is the global count of all block requests currently in
	// flight across all peers.
	inflightRequests int

	// eventQueue is the central channel for receiving events form peers to be
	// processed by the scheduler's event loop.
	eventQueue chan Event

	peerStateMut sync.RWMutex

	// peerState tracks the state of all currently connected peers, keyed by their
	// network address.
	peerState map[netip.AddrPort]*peerState
}

type Opts struct {
	Config      *Config
	Log         *slog.Logger
	PieceHashes [][sha1.Size]byte
	PieceLength int32
	TotalSize   int64
}

func NewPieceScheduler(opts Opts) (*PieceScheduler, error) {
	if opts.Config == nil {
		opts.Config = WithDefaultConfig()
	}

	n := len(opts.PieceHashes)
	availability := newAvailabilityBucket(n)

	totalBlocks := 0
	lastPieceLen := LastPieceLength(opts.TotalSize, opts.PieceLength)
	pieces := make([]*piece, n)

	for i := 0; i < n; i++ {
		plen, _ := PieceLengthAt(i, opts.TotalSize, opts.PieceLength)
		blockCount := BlocksInPiece(plen)
		totalBlocks += blockCount
		blocks := make([]*block, blockCount)

		for j := 0; j < blockCount; j++ {
			blocks[j] = &block{status: blockWant}
		}

		pieces[i] = &piece{
			index:       i,
			doneBlocks:  0,
			length:      plen,
			verified:    false,
			blocks:      blocks,
			isLastPiece: i == n-1,
			blockCount:  blockCount,
			sha:         opts.PieceHashes[i],
			lastBlock:   LastBlockInPiece(plen),
		}
	}

	return &PieceScheduler{
		nextPiece:       0,
		nextBlock:       0,
		pieceCount:      n,
		endgame:         false,
		pieces:          pieces,
		remainingBlocks: totalBlocks,
		cfg:             opts.Config,
		availability:    availability,
		lastPieceLen:    lastPieceLen,
		bitfield:        bitfield.New(n),
		eventQueue:      make(chan Event, 1000),
		peerState:       make(map[netip.AddrPort]*peerState),
		log:             opts.Log.With("component", "scheduler"),
	}, nil
}

func (s *PieceScheduler) Run(ctx context.Context) error {
	s.log.Debug("piece scheduler event loop started")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Info("piece scheduler shutting down", "reason", ctx.Err().Error())
			return nil

		case event, ok := <-s.eventQueue:
			if !ok {
				s.log.Debug("event queue closed, scheduler stopping")
				return nil
			}

			s.handleEvent(event)

		case <-ticker.C:
			s.findWorkForIdlePeers()
		}
	}
}

func (s *PieceScheduler) Bitfield() bitfield.Bitfield {
	s.mut.RLock()
	defer s.mut.RUnlock()

	return s.bitfield
}

func (s *PieceScheduler) GetPeerWorkQueue(peer netip.AddrPort) <-chan *PieceRequest {
	s.peerStateMut.RLock()
	if peerState, ok := s.peerState[peer]; ok {
		s.peerStateMut.RUnlock()
		return peerState.workQueue
	}
	s.peerStateMut.RUnlock()

	s.peerStateMut.Lock()
	defer s.peerStateMut.Unlock()

	if peerState, ok := s.peerState[peer]; ok {
		return peerState.workQueue
	}

	peerState := newPeerState(peer, s.pieceCount, s.cfg.maxRequestBacklog)
	s.peerState[peer] = peerState
	return peerState.workQueue
}

func (s *PieceScheduler) GetEventQueue() chan<- Event {
	return s.eventQueue
}

func (s *PieceScheduler) findAvailableBlock(piece *piece) (int, bool) {
	for i := 0; i < piece.blockCount; i++ {
		if piece.blocks[i].status == blockWant {
			return i, true
		}
	}

	return 0, false
}

func (s *PieceScheduler) resetBlockToWant(piece int, blockIdx int) {
	if !s.isPieceNeeded(piece) {
		return
	}

	p := s.pieces[piece]
	if blockIdx >= 0 && blockIdx < len(p.blocks) {
		block := p.blocks[blockIdx]
		if block.status == blockInflight {
			block.status = blockWant
			s.inflightRequests--
		}
	}
}

func (s *PieceScheduler) assignBlockToPeer(peer *peerState, pieceIdx, blockIdx int) {
	piece := s.pieces[pieceIdx]
	block := piece.blocks[blockIdx]

	begin, length, err := BlockBounds(piece.length, blockIdx)
	if err != nil {
		s.log.Error("invalid block bounds", "piece", pieceIdx, "block", blockIdx)
		return
	}

	block.status = blockInflight
	block.owner = &blockOwner{peer: peer.addr, requestedAt: time.Now()}

	peer.inflight++
	key := blockKey(pieceIdx, int(begin))
	peer.blockAssignments[key] = struct{}{}

	s.peerStateMut.Lock()
	defer s.peerStateMut.Unlock()

	s.inflightRequests++
	s.remainingBlocks--

	req := &PieceRequest{Piece: pieceIdx, Begin: int(begin), Length: int(length)}

	select {
	case peer.workQueue <- req:

	default:
		s.log.Warn("work queue full, dropping request", "peer", peer.addr)

		block.status = blockWant
		block.owner = nil
		peer.inflight--
		delete(peer.blockAssignments, key)
		s.inflightRequests--
		s.remainingBlocks++
	}
}

func (s *PieceScheduler) unassignBlockFromPeer(peer netip.AddrPort, piece, begin int) {
	key := blockKey(piece, begin)

	s.peerStateMut.Lock()
	defer s.peerStateMut.Unlock()

	ps, ok := s.peerState[peer]
	if !ok {
		s.log.Warn("unassign block from peer failed; not found!",
			"peer", peer,
			"piece", piece,
			"begin", begin,
		)
		return
	}

	delete(ps.blockAssignments, key)
	ps.inflight--
}

func (s *PieceScheduler) isBlockAssignedtoPeer(peer netip.AddrPort, piece, begin int) bool {
	s.peerStateMut.RLock()
	defer s.peerStateMut.RUnlock()

	ps, ok := s.peerState[peer]
	if !ok {
		s.log.Warn("is block assigned to peer failed; not found!",
			"peer", peer,
			"piece", piece,
			"begin", begin,
		)
		return false
	}

	key := blockKey(piece, begin)
	_, assigned := ps.blockAssignments[key]
	return assigned
}

func (s *PieceScheduler) updatePieceAvailability(peerBF bitfield.Bitfield, delta int) {
	s.mut.RLock()
	weHave := s.bitfield.Clone()
	s.mut.RUnlock()

	for i := 0; i < s.pieceCount; i++ {
		if peerBF.Has(i) && !weHave.Has(i) {
			s.availability.Move(i, delta)
		}
	}
}

func (s *PieceScheduler) isPieceNeeded(piece int) bool {
	if piece < 0 || piece >= s.pieceCount {
		return false
	}

	return !s.bitfield.Has(piece) && !s.pieces[piece].verified
}

func (s *PieceScheduler) findWorkForIdlePeers() {
	candidates := make([]netip.AddrPort, 0, len(s.peerState))

	s.peerStateMut.RLock()
	for addr, ps := range s.peerState {
		if !ps.choked && ps.inflight < s.cfg.MaxInflightRequestsPerPeer {
			candidates = append(candidates, addr)
		}
	}
	s.peerStateMut.RUnlock()

	for _, addr := range candidates {
		s.nextForPeer(addr)
	}
}
