package peer

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/prxssh/rabbit/internal/protocol"
	"github.com/prxssh/rabbit/internal/scheduler"
	"github.com/prxssh/rabbit/pkg/bitfield"
	"golang.org/x/sync/errgroup"
)

const (
	stateAmChoking      = 1 << 0
	stateAmInterested   = 1 << 1
	statePeerChoking    = 1 << 2
	statePeerInterested = 1 << 3
)

type Peer struct {
	cfg            *Config
	conn           net.Conn
	addr           netip.AddrPort
	log            *slog.Logger
	stats          *peerStats
	messageOutbox  chan *protocol.Message
	state          uint32
	lastActivityNs atomic.Int64
	workQueue      <-chan *scheduler.WorkItem
	eventQueue     chan<- scheduler.Event
	cancel         context.CancelFunc
	stopped        atomic.Bool
}

type peerStats struct {
	Downloaded        atomic.Uint64
	Uploaded          atomic.Uint64
	DownloadRate      atomic.Uint64
	UploadRate        atomic.Uint64
	MessagesReceived  atomic.Uint64
	MessagesSent      atomic.Uint64
	RequestsSent      atomic.Uint64
	RequestsReceived  atomic.Uint64
	RequestsCancelled atomic.Uint64
	RequestsTimeout   atomic.Uint64
	PiecesReceived    atomic.Uint64
	PiecesSent        atomic.Uint64
	Errors            atomic.Uint64
	ConnectedAt       time.Time
	DisconnectedAt    time.Time
}

type PeerMetrics struct {
	Addr           netip.AddrPort
	Downloaded     uint64
	Uploaded       uint64
	RequestsSent   uint64
	BlocksReceived uint64
	BlocksFailed   uint64
	LastActive     time.Time
	ConnectedAt    time.Time
	ConnectedForNs int64
	DownloadRate   uint64
	UploadRate     uint64
	IsChoked       bool
	IsInterested   bool
}

type peerOpts struct {
	log        *slog.Logger
	pieceCount int
	infoHash   [sha1.Size]byte
	clientID   [sha1.Size]byte
	workQueue  <-chan *scheduler.WorkItem
	eventQueue chan<- scheduler.Event
	config     *Config
}

func NewPeer(ctx context.Context, addr netip.AddrPort, opts *peerOpts) (*Peer, error) {
	log := opts.log.With("src", "peer", "addr", addr)

	conn, err := net.DialTimeout("tcp", addr.String(), opts.config.DialTimeout)
	if err != nil {
		return nil, err
	}

	handshake := protocol.NewHandshake(opts.infoHash, opts.clientID)
	if _, err := handshake.Exchange(conn, true); err != nil {
		_ = conn.Close()
		return nil, err
	}

	p := &Peer{
		cfg:           opts.config,
		log:           log,
		conn:          conn,
		addr:          addr,
		stats:         &peerStats{},
		workQueue:     opts.workQueue,
		eventQueue:    opts.eventQueue,
		messageOutbox: make(chan *protocol.Message, opts.config.PeerOutboxBacklog),
	}
	p.setState(stateAmChoking|statePeerChoking, true)
	p.lastActivityNs.Store(time.Now().UnixNano())
	p.stats.ConnectedAt = time.Now()
	p.eventQueue <- scheduler.NewHandshakeEVent(p.addr)

	return p, nil
}

func (p *Peer) Run(ctx context.Context) error {
	defer p.cleanup()

	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return p.readMessagesLoop(gctx) })
	g.Go(func() error { return p.writeMessagesLoop(gctx) })
	g.Go(func() error { return p.requestWorkerLoop(gctx) })
	g.Go(func() error { return p.downloadUploadRatesLoop(gctx) })

	return g.Wait()
}

func (p *Peer) Close() {
	if !p.stopped.CompareAndSwap(false, true) {
		return
	}

	if p.cancel != nil {
		p.cancel()
	}

	_ = p.conn.Close()
}

func (p *Peer) AmChoking() bool      { return p.getState(stateAmChoking) }
func (p *Peer) AmInterested() bool   { return p.getState(stateAmInterested) }
func (p *Peer) PeerChoking() bool    { return p.getState(statePeerChoking) }
func (p *Peer) PeerInterested() bool { return p.getState(statePeerInterested) }

func (p *Peer) Idleness() time.Duration {
	ns := time.Unix(0, p.lastActivityNs.Load())
	return time.Since(ns)
}

func (p *Peer) Stats() PeerMetrics {
	lastNs := p.lastActivityNs.Load()
	lastActive := time.Unix(0, lastNs)
	connectedAt := p.stats.ConnectedAt

	return PeerMetrics{
		Addr:           p.addr,
		Downloaded:     p.stats.Downloaded.Load(),
		Uploaded:       p.stats.Uploaded.Load(),
		RequestsSent:   p.stats.RequestsSent.Load(),
		BlocksReceived: p.stats.PiecesReceived.Load(),
		BlocksFailed:   p.stats.RequestsTimeout.Load(),
		LastActive:     lastActive,
		ConnectedAt:    connectedAt,
		DownloadRate:   p.stats.DownloadRate.Load(),
		UploadRate:     p.stats.UploadRate.Load(),
		IsChoked:       p.PeerChoking(),
		IsInterested:   p.AmInterested(),
	}
}

func (p *Peer) cleanup() {
	p.stopped.Store(true)

	close(p.messageOutbox)

	p.stats.DisconnectedAt = time.Now()
	p.eventQueue <- scheduler.NewPeerGoneEvent(p.addr)
}

func (p *Peer) readMessagesLoop(ctx context.Context) error {
	l := p.log.With("component", "read message loop")
	l.Debug("started")

	for {
		select {
		case <-ctx.Done():
			l.Warn("context done!", "error", ctx.Err().Error())
			return nil
		default:
		}

		message, err := p.readMessage()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}

			l.Warn("failed to read message, exiting!", "error", err.Error())
			return err
		}

		if err := p.handleMessage(message); err != nil {
			l.Warn("handle message failed", "error", err.Error())
			return err
		}
	}
}

func (p *Peer) writeMessagesLoop(ctx context.Context) error {
	l := p.log.With("component", "write messages loop")
	l.Debug("started")

	heartbeatTicker := time.NewTicker(p.cfg.PeerHeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Warn("exiting; context done!", "error", ctx.Err().Error())
			return nil

		case message, ok := <-p.messageOutbox:
			if !ok {
				l.Warn("exiting; outbox is closed")
				return nil
			}

			if err := p.writeMessage(message); err != nil {
				l.Warn(
					"failed to write message, exiting loop",
					"error", err.Error(),
				)
				return err
			}

		case <-heartbeatTicker.C:
			lastActivityAt := time.Unix(0, p.lastActivityNs.Load())

			if time.Since(lastActivityAt) >= p.cfg.PeerHeartbeatInterval {
				p.pushMessage(ctx, nil)
			}
		}
	}
}

// Rate calculation (UploadRate / DownloadRate)
//
// We maintain two monotonic byte counters per peer: Uploaded and Downloaded.
// A 1s ticker snapshots these totals and computes a delta from the previous
// snapshot. The delta over the tick interval is the instantaneous throughput
// in bytes/sec:
//
//	instant = (curTotal - lastTotal) / elapsedSeconds
//
// To reduce jitter, we smooth the instantaneous value with an exponential
// moving average (EMA):
//
//	emaNext = α*instant + (1-α)*emaPrev, where 0<α≤1.

// Higher α reacts faster; lower α is smoother. If you prefer a raw
// per-second rate, set α=1 (emaNext == instant).
//
// Notes:
//   - Counters only increase; unsigned subtraction yields the correct delta.
//   - If the ticker drifts, divide by the measured elapsedSeconds instead of
//     assuming exactly 1s.
//   - Store the final bytes/sec into UploadRate and DownloadRate atomically.
//   - Pauses naturally produce zero deltas (zero rate).
func (p *Peer) downloadUploadRatesLoop(ctx context.Context) error {
	l := p.log.With("component", "download upload rate loop")
	l.Debug("started")

	t := time.NewTicker(time.Second)
	defer t.Stop()

	lastUp := p.stats.Uploaded.Load()
	lastDown := p.stats.Downloaded.Load()
	lastTick := time.Now()

	var (
		upEMA   uint64
		downEMA uint64
		inited  bool
	)

	for {
		select {
		case <-ctx.Done():
			l.Warn("context done!", "error", ctx.Err().Error())
			return nil

		case now := <-t.C:
			elapsed := now.Sub(lastTick).Seconds()
			curUp := p.stats.Uploaded.Load()
			curDown := p.stats.Downloaded.Load()

			instUpRate := uint64(float64(curUp-lastUp) / elapsed)
			instDownRate := uint64(float64(curDown-lastDown) / elapsed)

			if !inited {
				upEMA = instUpRate
				downEMA = instDownRate
				inited = true
			} else {
				upEMA = (1*instUpRate + 4*upEMA) / 5
				downEMA = (1*instDownRate + 4*downEMA) / 5
			}

			p.stats.UploadRate.Store(upEMA)
			p.stats.DownloadRate.Store(downEMA)

			lastUp = curUp
			lastDown = curDown
			lastTick = now
		}
	}
}

func (p *Peer) requestWorkerLoop(ctx context.Context) error {
	l := p.log.With("component", "request worker loop")
	l.Debug("started")

	for {
		select {
		case <-ctx.Done():
			return nil

		case work, ok := <-p.workQueue:
			if !ok {
				return nil
			}

			var message *protocol.Message

			switch work.Type {
			case scheduler.WorkRequestPiece:
				message = protocol.MessageRequest(
					uint32(work.Piece),
					uint32(work.Begin),
					uint32(work.Length),
				)
			case scheduler.WorkSendHave:
				message = protocol.MessageHave(uint32(work.Piece))
			case scheduler.WorkSendBitfield:
				message = protocol.MessageBitfield(work.Bitfield)
			case scheduler.WorkSendNotInterested:
				message = protocol.MessageNotInterested()
			case scheduler.WorkSendInterested:
				message = protocol.MessageInterested()
			case scheduler.WorkCancelPiece:
				message = protocol.MessageCancel(
					work.Piece,
					work.Begin,
					work.Length,
				)
			}

			p.pushMessage(ctx, message)
		}
	}
}

func (p *Peer) readMessage() (*protocol.Message, error) {
	_ = p.conn.SetReadDeadline(time.Now().Add(p.cfg.ReadTimeout))
	defer p.conn.SetReadDeadline(time.Time{})

	message, err := protocol.ReadMessage(p.conn)
	if err != nil {
		p.stats.Errors.Add(1)
		return nil, err
	}

	p.stats.MessagesReceived.Add(1)
	p.lastActivityNs.Store(time.Now().UnixNano())

	return message, nil
}

func (p *Peer) writeMessage(message *protocol.Message) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(p.cfg.WriteTimeout))
	defer p.conn.SetWriteDeadline(time.Time{})

	if err := protocol.WriteMessage(p.conn, message); err != nil {
		p.stats.Errors.Add(1)
		return err
	}

	p.onMessageWritten(message)
	return nil
}

func (p *Peer) getState(state uint32) bool { return atomic.LoadUint32(&p.state)&state != 0 }

func (p *Peer) setState(state uint32, on bool) {
	for {
		old := atomic.LoadUint32(&p.state)
		var new uint32
		if on {
			new = old | state
		} else {
			new = old &^ state
		}

		if atomic.CompareAndSwapUint32(&p.state, old, new) {
			return
		}
	}
}

func (p *Peer) handleMessage(message *protocol.Message) error {
	if protocol.IsKeepAlive(message) {
		return nil
	}

	switch message.ID {
	case protocol.Choke:
		p.setState(statePeerChoking, true)
		p.eventQueue <- scheduler.NewChokedEvent(p.addr)

	case protocol.Unchoke:
		p.setState(statePeerChoking, false)
		p.eventQueue <- scheduler.NewUnchokedEvent(p.addr)

	case protocol.Interested:
		p.setState(statePeerInterested, true)

	case protocol.NotInterested:
		p.setState(statePeerInterested, false)

	case protocol.Bitfield:
		bf := bitfield.FromBytes(message.Payload)
		p.eventQueue <- scheduler.NewBitfieldEvent(p.addr, bf)

	case protocol.Have:
		piece, ok := message.ParseHave()
		if !ok {
			return errors.New("malformed have message")
		}

		p.eventQueue <- scheduler.NewHaveEvent(p.addr, piece)

	case protocol.Piece:
		piece, begin, block, ok := message.ParsePiece()
		if !ok {
			return errors.New("malformed piece message")
		}

		p.eventQueue <- scheduler.NewPieceEvent(p.addr, piece, begin, block)

		p.stats.PiecesReceived.Add(1)
		p.stats.Downloaded.Add(uint64(len(block)))

	case protocol.Request:
		_, _, _, ok := message.ParseRequest()
		if !ok {
			return errors.New("malformed request message")
		}

		p.stats.RequestsReceived.Add(1)

	case protocol.Cancel:
		p.stats.RequestsCancelled.Add(1)

	default:
		return fmt.Errorf("invalid message id '%d'", message.ID)
	}

	return nil
}

func (p *Peer) onMessageWritten(message *protocol.Message) {
	p.stats.MessagesSent.Add(1)
	p.lastActivityNs.Store(time.Now().UnixNano())

	if message == nil {
		return
	}

	switch message.ID {
	case protocol.Choke:
		p.setState(stateAmChoking, true)

	case protocol.Unchoke:
		p.setState(stateAmChoking, false)

	case protocol.Interested:
		p.setState(stateAmInterested, true)

	case protocol.NotInterested:
		p.setState(stateAmInterested, false)

	case protocol.Have:
		// nothing to do

	case protocol.Bitfield:
		// nothing to do

	case protocol.Request:
		p.stats.RequestsSent.Add(1)

	case protocol.Piece:
		// Piece upload truly happened; count piece + payload bytes
		// Payload layout: 4(index) + 4(begin) + <block>
		if n := len(message.Payload); n >= 8 {
			blockLen := n - 8
			p.stats.PiecesSent.Add(1)
			p.stats.Uploaded.Add(uint64(blockLen))
		}

	case protocol.Cancel:
		p.stats.RequestsCancelled.Add(1)

	default:
		// unknown ID; nothing to do
	}
}

func (p *Peer) pushMessage(ctx context.Context, message *protocol.Message) {
	select {
	case p.messageOutbox <- message:

	case <-ctx.Done():
		return
	}
}
