package peer

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prxssh/rabbit/internal/config"
	"github.com/prxssh/rabbit/internal/protocol"
	"github.com/prxssh/rabbit/internal/utils/bitfield"
	"golang.org/x/sync/errgroup"
)

const (
	maskAmChoking      = 1 << 0
	maskAmInterested   = 1 << 1
	maskPeerChoking    = 1 << 2
	maskPeerInterested = 1 << 3
)

type Peer struct {
	log           *slog.Logger
	conn          net.Conn
	addr          netip.AddrPort
	state         uint32
	stats         *PeerStats
	bitfieldMu    sync.RWMutex
	bitfield      bitfield.Bitfield
	lastAcitivyAt atomic.Int64
	outbox        chan *protocol.Message
	closeOnce     sync.Once
	startOnce     sync.Once
	stopped       atomic.Bool
	cancel        context.CancelFunc
	onBitfield    func(netip.AddrPort, bitfield.Bitfield)
	onHave        func(netip.AddrPort, int)
	onDisconnect  func(netip.AddrPort)
	onHandshake   func(netip.AddrPort)
	onPiece       func(netip.AddrPort, int, int, []byte)
	requestWork   func(netip.AddrPort)
}

// PeerStats holds per-connection counters/timestamps. All counters are
// atomic and monotonically increasing for the lifetime of a peer.
type PeerStats struct {
	// Downloaded is the total number of BYTES we have received from this
	// peer.
	Downloaded atomic.Uint64

	// Uploaded is the total number of BYTES we have sent to this peer.
	Uploaded atomic.Uint64

	// DownloadRate is an instantaneous or smoothed BYTES PER SECOND estimate
	// of incoming data.
	DownloadRate atomic.Uint64

	// UploadRate is an instantaneous or smoothed BYTES PER SECOND estimate of
	// outgoing data.
	UploadRate atomic.Uint64

	// MessagesReceived counts frames successfully READ from the socket,
	// including keep-alives.
	MessagesReceived atomic.Uint64

	// MessagesSent counts frames successfully WRITTEN to the socket,
	// including keep-alives.
	MessagesSent atomic.Uint64

	// RequestsSent counts REQUEST messages we successfully wrote to the
	// socket.
	RequestsSent atomic.Uint64

	// RequestsReceived counts REQUEST messages received from the peer.
	RequestsReceived atomic.Uint64

	// RequestsCancelled is the total number of CANCELs (both directions).
	RequestsCancelled atomic.Uint64

	// RequestsTimeout counts our detected timeouts for requests we sent to
	// this peer.
	RequestsTimeout atomic.Uint64

	// PiecesReceived counts PIECE messages we received (i.e., completed
	// blocks from the peer).
	PiecesReceived atomic.Uint64

	// PiecesSent counts PIECE messages we successfully wrote (i.e., blocks
	// uploaded to the peer).
	PiecesSent atomic.Uint64

	// Errors counts protocol or I/O errors local to this peer connection
	// (failed reads/writes, malformed messages, etc.).
	Errors atomic.Uint64

	// ConnectedAt is the wall-clock time when the TCP connection and
	// handshake succeeded.
	ConnectedAt time.Time

	// DisconnectedAt is the wall-clock time when the connection was
	// closed (local or remote).
	DisconnectedAt time.Time
}

// PeerMetrics is a snapshot of a single peer's connection + transfer stats.
// Exported for binding to the frontend via Wails.
type PeerMetrics struct {
	Addr           netip.AddrPort
	Downloaded     uint64
	Uploaded       uint64
	RequestsSent   uint64
	BlocksReceived uint64
	BlocksFailed   uint64
	LastActive     time.Time
	ConnectedAt    time.Time
	ConnectedFor   int64 // duration in nanoseconds
	DownloadRate   uint64
	UploadRate     uint64
	IsChoked       bool
	IsInterested   bool
}

type PeerOpts struct {
	Log          *slog.Logger
	PieceCount   int
	InfoHash     [sha1.Size]byte
	OnBitfield   func(netip.AddrPort, bitfield.Bitfield)
	OnHave       func(netip.AddrPort, int)
	OnDisconnect func(netip.AddrPort)
	OnHandshake  func(netip.AddrPort)
	OnPiece      func(netip.AddrPort, int, int, []byte)
	RequestWork  func(netip.AddrPort)
}

func NewPeer(ctx context.Context, addr netip.AddrPort, opts *PeerOpts) (*Peer, error) {
	log := opts.Log.With("src", "peer", "addr", addr)

	conn, err := net.DialTimeout("tcp", addr.String(), config.Load().DialTimeout)
	if err != nil {
		return nil, err
	}

	handshake := protocol.NewHandshake(opts.InfoHash, config.Load().ClientID)
	if _, err := handshake.Exchange(conn, true); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if opts.OnHandshake != nil {
	}

	p := &Peer{
		log:          log,
		conn:         conn,
		addr:         addr,
		stats:        &PeerStats{},
		onBitfield:   opts.OnBitfield,
		onHave:       opts.OnHave,
		onDisconnect: opts.OnDisconnect,
		onHandshake:  opts.OnHandshake,
		onPiece:      opts.OnPiece,
		requestWork:  opts.RequestWork,
		bitfield:     bitfield.New(opts.PieceCount),
		outbox:       make(chan *protocol.Message, config.Load().PeerOutboundQueueBacklog),
	}
	p.setState(maskAmChoking|maskPeerChoking, true)
	p.lastAcitivyAt.Store(time.Now().UnixNano())
	p.stats.ConnectedAt = time.Now()

	return p, nil
}

func (p *Peer) Run(ctx context.Context) error {
	defer p.Close()

	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return p.readMessagesLoop(gctx) })
	g.Go(func() error { return p.writeMessagesLoop(gctx) })
	g.Go(func() error { return p.downloadUploadRatesLoop(gctx) })

	return g.Wait()
}

func (p *Peer) Close() {
	p.closeOnce.Do(func() {
		p.stopped.Store(true)

		if p.cancel != nil {
			p.cancel()
		}

		_ = p.conn.Close()
		close(p.outbox)
		p.stats.DisconnectedAt = time.Now()

		p.log.Debug("stopped peer")
	})
}

func (p *Peer) Idleness() time.Duration {
	ns := time.Unix(0, p.lastAcitivyAt.Load())
	return time.Since(ns)
}

func (p *Peer) SendBitfield(bf bitfield.Bitfield) {
	p.enqueueMessage(protocol.MessageBitfield(bf.Bytes()))
}

func (p *Peer) SendKeepAlive() {
	p.enqueueMessage(nil)
}

func (p *Peer) SendChoke() {
	p.enqueueMessage(protocol.MessageChoke())
}

func (p *Peer) SendUnchoke() {
	p.enqueueMessage(protocol.MessageUnchoke())
}

func (p *Peer) SendInterested() {
	p.enqueueMessage(protocol.MessageInterested())
}

func (p *Peer) SendNotInterested() {
	p.enqueueMessage(protocol.MessageNotInterested())
}

func (p *Peer) SendHave(piece uint32) {
	p.enqueueMessage(protocol.MessageHave(piece))
}

func (p *Peer) SendCancel(piece, begin, length int) {
	p.enqueueMessage(protocol.MessageCancel(piece, begin, length))
}

func (p *Peer) SendRequest(piece, begin, length int) {
	if p.PeerChoking() {
		return
	}

	p.enqueueMessage(protocol.MessageRequest(uint32(piece), uint32(begin), uint32(length)))
}

func (p *Peer) SendPiece(piece, begin uint32, block []byte) {
	if p.PeerChoking() {
		return
	}

	p.enqueueMessage(protocol.MessagePiece(piece, begin, block))
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

	p.onHandshake(p.addr)

	keepAliveInterval := config.Load().KeepAliveInterval
	ticker := time.NewTicker(keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Warn("exiting; context done!", "error", ctx.Err().Error())
			return nil

		case message, ok := <-p.outbox:
			if !ok {
				l.Warn("exiting; outbox is closed")
				return nil
			}

			l.Debug("writing message", "message", message.ID.String())

			if err := p.writeMessage(message); err != nil {
				l.Warn(
					"failed to write message, exiting loop",
					"error", err.Error(),
				)
				return err
			}

		case <-ticker.C:
			lastAcitivyAt := time.Unix(0, p.lastAcitivyAt.Load())

			if time.Since(lastAcitivyAt) >= keepAliveInterval {
				p.SendKeepAlive()
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
//	emaNext = α*instant + (1-α)*emaPrev
//
// where 0<α≤1. Higher α reacts faster; lower α is smoother. If you prefer a
// raw per-second rate, set α=1 (emaNext == instant).
//
// Notes:
//   - Counters only increase; unsigned subtraction yields the correct delta.
//   - If the ticker drifts, divide by the measured elapsedSeconds instead of
//     assuming exactly 1s.
//   - Store the final bytes/sec into UploadRate and DownloadRate atomically.
//   - Pauses naturally produce zero deltas (zero rate).
func (p *Peer) downloadUploadRatesLoop(ctx context.Context) error {
	l := p.log.With("component", "download-upload rate loop")
	l.Debug("started")

	t := time.NewTicker(time.Second)
	defer t.Stop()

	lastUp := p.stats.Uploaded.Load()
	lastDown := p.stats.Downloaded.Load()

	const alpha = 0.2
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
		case <-t.C:
			curUp := p.stats.Uploaded.Load()
			curDown := p.stats.Downloaded.Load()

			instUp := curUp - lastUp
			instDown := curDown - lastDown

			if !inited {
				upEMA = instUp
				downEMA = instDown
				inited = true
			} else {
				upEMA = uint64(alpha*float64(instUp) + (1-alpha)*float64(upEMA))
				downEMA = uint64(alpha*float64(instDown) + (1-alpha)*float64(downEMA))
			}

			p.stats.UploadRate.Store(upEMA)
			p.stats.DownloadRate.Store(downEMA)

			// Update baseline for next iteration
			lastUp = curUp
			lastDown = curDown
		}
	}
}

func (p *Peer) readMessage() (*protocol.Message, error) {
	_ = p.conn.SetReadDeadline(time.Now().Add(config.Load().ReadTimeout))
	defer p.conn.SetReadDeadline(time.Time{})

	message, err := protocol.ReadMessage(p.conn)
	if err != nil {
		p.stats.Errors.Add(1)
		return nil, err
	}

	p.stats.MessagesReceived.Add(1)
	p.lastAcitivyAt.Store(time.Now().UnixNano())

	return message, nil
}

func (p *Peer) writeMessage(message *protocol.Message) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(config.Load().WriteTimeout))
	defer p.conn.SetWriteDeadline(time.Time{})

	if err := protocol.WriteMessage(p.conn, message); err != nil {
		p.stats.Errors.Add(1)
		return err
	}

	p.onMessageWritten(message)
	return nil
}

func (p *Peer) AmChoking() bool      { return p.getState(maskAmChoking) }
func (p *Peer) AmInterested() bool   { return p.getState(maskAmInterested) }
func (p *Peer) PeerChoking() bool    { return p.getState(maskPeerChoking) }
func (p *Peer) PeerInterested() bool { return p.getState(maskPeerInterested) }

func (p *Peer) getState(mask uint32) bool { return atomic.LoadUint32(&p.state)&mask != 0 }

func (p *Peer) setState(mask uint32, on bool) {
	for {
		old := atomic.LoadUint32(&p.state)
		var new uint32
		if on {
			new = old | mask
		} else {
			new = old &^ mask
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
	case protocol.MsgChoke:
		p.setState(maskPeerChoking, true)
	case protocol.MsgUnchoke:
		p.setState(maskPeerChoking, false)
		p.requestWork(p.addr)
	case protocol.MsgInterested:
		p.setState(maskPeerInterested, true)
	case protocol.MsgNotInterested:
		p.setState(maskPeerInterested, false)
	case protocol.MsgBitfield:
		bf := bitfield.FromBytes(message.Payload)
		p.onBitfield(p.addr, bf)
	case protocol.MsgHave:
		piece, ok := message.ParseHave()
		if !ok {
			return errors.New("malformed have message")
		}
		p.onHave(p.addr, int(piece))

	case protocol.MsgPiece:
		piece, begin, block, ok := message.ParsePiece()
		if !ok {
			return errors.New("malformed piece message")
		}

		p.onPiece(p.addr, int(piece), int(begin), block)
		p.stats.PiecesReceived.Add(1)
		p.stats.Downloaded.Add(uint64(len(block)))
	case protocol.MsgRequest:
		_, _, _, ok := message.ParseRequest()
		if !ok {
			return errors.New("malformed request message")
		}

		p.stats.RequestsReceived.Add(1)
	case protocol.MsgCancel:
		p.stats.RequestsCancelled.Add(1)
	default:
		return fmt.Errorf("invalid message id '%d'", message.ID)
	}

	return nil
}

func (p *Peer) enqueueMessage(message *protocol.Message) bool {
	if p.stopped.Load() {
		return false
	}

	select {
	case p.outbox <- message:
		return true
	default:
		return false
	}
}

func (p *Peer) onMessageWritten(message *protocol.Message) {
	p.stats.MessagesSent.Add(1)
	p.lastAcitivyAt.Store(time.Now().UnixNano())

	if message == nil {
		return
	}

	switch message.ID {
	case protocol.MsgChoke:
		p.setState(maskAmChoking, true)

	case protocol.MsgUnchoke:
		p.setState(maskAmChoking, false)

	case protocol.MsgInterested:
		p.setState(maskAmInterested, true)

	case protocol.MsgNotInterested:
		p.setState(maskAmInterested, false)

	case protocol.MsgHave:
		// nothing to do

	case protocol.MsgBitfield:
		// nothing to do

	case protocol.MsgRequest:
		p.stats.RequestsSent.Add(1)

	case protocol.MsgPiece:
		// Piece upload truly happened; count piece + payload bytes
		// Payload layout: 4(index) + 4(begin) + <block>
		if n := len(message.Payload); n >= 8 {
			blockLen := n - 8
			p.stats.PiecesSent.Add(1)
			p.stats.Uploaded.Add(uint64(blockLen))
		}

	case protocol.MsgCancel:
		p.stats.RequestsCancelled.Add(1)

	default:
		// unknown ID; nothing to do
	}
}

// Stats returns a snapshot of metrics for this peer.
func (p *Peer) Stats() PeerMetrics {
	lastNs := p.lastAcitivyAt.Load()
	lastActive := time.Unix(0, lastNs)
	connectedAt := p.stats.ConnectedAt
	connectedFor := time.Since(connectedAt).Nanoseconds()

	return PeerMetrics{
		Addr:           p.addr,
		Downloaded:     p.stats.Downloaded.Load(),
		Uploaded:       p.stats.Uploaded.Load(),
		RequestsSent:   p.stats.RequestsSent.Load(),
		BlocksReceived: p.stats.PiecesReceived.Load(),
		BlocksFailed:   p.stats.RequestsTimeout.Load(),
		LastActive:     lastActive,
		ConnectedAt:    connectedAt,
		ConnectedFor:   connectedFor,
		DownloadRate:   p.stats.DownloadRate.Load(),
		UploadRate:     p.stats.UploadRate.Load(),
		IsChoked:       p.PeerChoking(),
		IsInterested:   p.AmInterested(),
	}
}
