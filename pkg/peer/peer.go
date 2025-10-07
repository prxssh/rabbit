package peer

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prxssh/rabbit/pkg/config"
	"github.com/prxssh/rabbit/pkg/piece"
	"github.com/prxssh/rabbit/pkg/utils/bitfield"
	"golang.org/x/sync/errgroup"
)

// Peer represents a single BitTorrent peer connection.
//
// It manages the protocol state machine (choke/interested), handles message
// exchange, and tracks performance metrics for the connection.
type Peer struct {
	m    *Manager
	log  *slog.Logger
	conn net.Conn
	addr netip.AddrPort

	// Protocol state
	amChoking      bool // are we choking this peer?
	amInterested   bool // are we interested in this peer?
	peerChoking    bool // is the peer choking us?
	peerInterested bool // is the peer interested in us?

	// bf is the peer's bitfield indicating which pieces they have.
	bf bitfield.Bitfield

	// outq buffers outgoing messages for asynchronous sending
	outq chan *Message

	// Performance metrics
	statsMu         sync.RWMutex
	connectedAt     time.Time
	downloadedBytes int64
	uploadedBytes   int64
	requestsSent    int
	blocksReceived  int
	blocksFailed    int
	lastActiveAt    time.Time

	closed atomic.Bool
}

// PeerStats represents performance metrics for a single peer connection.
type PeerStats struct {
	Addr           netip.AddrPort
	Downloaded     int64         // total bytes downloaded from this peer
	Uploaded       int64         // total bytes uploaded to this peer
	RequestsSent   int           // number of block requests sent
	BlocksReceived int           // number of blocks successfully received
	BlocksFailed   int           // number of failed/invalid blocks
	LastActive     time.Time     // timestamp of last message received
	ConnectedAt    time.Time     // when the connection was established
	ConnectedFor   time.Duration // how long the connection has been active
	DownloadRate   int64         // estimated download rate in bytes/sec
	IsChoked       bool          // true if peer is currently choking us
	IsInterested   bool          // true if we are currently interested in peer
}

// dialPeer establishes a TCP connection to a peer and performs the BitTorrent
// handshake.
func dialPeer(
	ctx context.Context,
	m *Manager,
	addr netip.AddrPort,
) (*Peer, error) {
	dialer := &net.Dialer{
		KeepAlive: config.Load().DialTimeout,
		Control:   nil,
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr.String())
	if err != nil {
		return nil, err
	}

	l := m.log.With("src", "peer", "addr", addr)
	l.Info("connected")

	_ = conn.SetReadDeadline(time.Now().Add(config.Load().ReadTimeout))
	_ = conn.SetWriteDeadline(time.Now().Add(config.Load().WriteTimeout))

	hs := NewHandshake(m.infoHash, m.clientID)
	if err := hs.Perform(conn); err != nil {
		l.Warn("handshake failed", "err", err.Error())

		_ = conn.Close()
		return nil, err
	}
	if err := WriteMessage(conn, MessageBitfield(m.pieceManager.Bitfield())); err != nil {
		l.Warn("send bitfield failed", "error", err.Error())

		_ = conn.Close()
		return nil, err

	}

	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})

	return &Peer{
		m:              m,
		log:            l,
		addr:           addr,
		conn:           conn,
		amChoking:      true,
		amInterested:   false,
		peerChoking:    true,
		peerInterested: false,
		connectedAt:    time.Now(),
		lastActiveAt:   time.Now(),
		bf:             bitfield.New(m.pieceCount),
		outq: make(
			chan *Message,
			config.Load().PeerOutboundQueueBacklog,
		),
	}, nil
}

func (p *Peer) LastActiveAt() time.Time {
	p.statsMu.RLock()
	defer p.statsMu.RUnlock()

	return p.lastActiveAt
}

// Stats returns a snapshot of this peer's current performance metrics.
func (p *Peer) Stats() PeerStats {
	p.statsMu.RLock()
	defer p.statsMu.RUnlock()

	connectedFor := time.Since(p.connectedAt)

	var downloadRate int64
	seconds := int64(connectedFor.Seconds())
	if seconds > 0 {
		downloadRate = p.downloadedBytes / seconds
	}

	return PeerStats{
		Addr:           p.addr,
		Downloaded:     p.downloadedBytes,
		Uploaded:       p.uploadedBytes,
		RequestsSent:   p.requestsSent,
		BlocksReceived: p.blocksReceived,
		BlocksFailed:   p.blocksFailed,
		LastActive:     p.lastActiveAt,
		ConnectedAt:    p.connectedAt,
		ConnectedFor:   connectedFor,
		DownloadRate:   downloadRate,
		IsChoked:       p.peerChoking,
		IsInterested:   p.peerInterested,
	}
}

// run executes the peer's read and write loops until the context is cancelled
// or an error occurs. This method blocks until the peer stops.
func (p *Peer) run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return p.readLoop(ctx) })
	eg.Go(func() error { return p.writeLoop(ctx) })

	err := eg.Wait()
	p.cleanup()

	return err
}

func (p *Peer) cleanup() {
	if !p.closed.CompareAndSwap(false, true) {
		return
	}

	close(p.outq)

	p.m.pieceManager.OnPeerGone(p.addr, p.bf)
	_ = p.conn.Close()
}

// readLoop continuously reads and processes messages from the peer until
// the context is cancelled or an error occurs.
func (p *Peer) readLoop(ctx context.Context) error {
	l := p.log.With("src", "read.loop")

	lastRecv := time.Now()

	for {
		select {
		case <-ctx.Done():
			l.Info("loop exit",
				"reason", "ctx",
				"error", ctx.Err().Error(),
			)
			return ctx.Err()
		default:
		}

		msg, err := p.readMessage()
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			if time.Since(
				lastRecv,
			) > config.Load().KeepAliveInterval {
				return context.DeadlineExceeded
			}
			continue
		}
		if err != nil {
			p.m.pieceManager.OnPeerGone(p.addr, p.bf)
			return err
		}

		if msg == nil { // keep-alive
			lastRecv = time.Now()
			continue
		}

		lastRecv = time.Now()

		p.statsMu.Lock()
		p.lastActiveAt = lastRecv
		p.statsMu.Unlock()

		switch msg.ID {
		case MsgChoke:
			p.peerChoking = true

		case MsgUnchoke:
			p.peerChoking = false
			p.requestNextPiece()

		case MsgInterested:
			p.peerInterested = true

		case MsgNotInterested:
			p.peerInterested = false

		case MsgBitfield:
			p.bf = bitfield.FromBytes(msg.Payload)
			p.m.pieceManager.OnPeerBitfield(p.addr, p.bf)

			if p.shouldBeInterested() && !p.amInterested {
				p.sendInterested()
			}
			p.requestNextPiece()

		case MsgHave:
			pieceIdx, ok := msg.ParseHave()
			if !ok {
				continue
			}

			p.bf.Set(int(pieceIdx))
			p.m.pieceManager.OnPeerHave(p.addr, int(pieceIdx))

			if p.shouldBeInterested() && !p.amInterested {
				p.sendInterested()
			}
			p.requestNextPiece()

		case MsgPiece:
			idx, begin, data, ok := msg.ParsePiece()
			if !ok {
				p.statsMu.Lock()
				p.blocksFailed++
				p.statsMu.Unlock()
				continue
			}

			dataLen := len(data)
			p.statsMu.Lock()
			p.blocksReceived++
			p.downloadedBytes += int64(dataLen)
			p.statsMu.Unlock()

			p.m.updateTotalDownloaded(dataLen)

			complete, _, err := p.m.pieceManager.OnBlockReceived(
				p.addr,
				int(idx),
				int(begin),
				data,
			)
			if err != nil {
				p.log.Warn(
					"block failure",
					"error", err,
					"piece_idx", idx,
					"begin", begin,
				)
				continue
			}
			p.requestNextPiece()

			if complete {
				l.Info("piece complete", "piece_idx", idx)
			}

		case MsgRequest:
			index, begin, length, ok := msg.ParseRequest()
			if !ok {
				continue
			}

			block, err := p.m.pieceManager.ReadPiece(
				int(index),
				int(begin),
				int(length),
			)
			if err != nil {
				p.log.Error("read piece failure",
					"error", err,
					"index", index,
					"begin", begin,
					"length", length,
				)
				continue
			}
			p.outq <- MessagePiece(int(index), int(begin), block)

		default:
			l.Warn("message unknown", "message", msg.ID)
		}

	}
}

// writeLoop continuously sends queued messages and keep-alives to the peer
// until the context is cancelled or an error occurs.
func (p *Peer) writeLoop(ctx context.Context) error {
	l := p.log.With("src", "write.loop")

	keepAliveTicker := time.NewTicker(config.Load().KeepAliveInterval)
	defer keepAliveTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Info(
				"exit",
				"reason", "ctx",
				"err", ctx.Err().Error(),
			)
			return ctx.Err()

		case msg, ok := <-p.outq:
			if !ok {
				l.Warn("outq closed")
				return nil
			}

			if err := p.writeMessage(msg); err != nil {
				l.Warn("write failed", "error", err.Error())
				return err
			}

		case <-keepAliveTicker.C:
			if err := p.writeMessage(nil); err != nil {
				l.Warn(
					"keepalive send error",
					"error", err.Error(),
				)
				return err
			}
		}
	}
}

func (p *Peer) writeMessage(message *Message) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(config.Load().WriteTimeout))
	defer p.conn.SetWriteDeadline(time.Time{})

	return WriteMessage(p.conn, message)
}

func (p *Peer) readMessage() (*Message, error) {
	_ = p.conn.SetReadDeadline(time.Now().Add(config.Load().ReadTimeout))
	defer p.conn.SetReadDeadline(time.Time{})

	return ReadMessage(p.conn)
}

func (p *Peer) sendInterested() {
	if p.closed.Load() {
		return
	}

	if p.amInterested {
		return
	}

	p.amInterested = true
	p.outq <- MessageInterested()
}

func (p *Peer) sendNotInterested() {
	if p.closed.Load() {
		return
	}

	if !p.amInterested {
		return
	}

	p.amInterested = false
	p.outq <- MessageNotInterested()
}

func (p *Peer) shouldBeInterested() bool {
	if p.closed.Load() {
		return false
	}

	idx, ok := p.m.pieceManager.CurrentPieceIndex()
	if !ok {
		return false
	}

	return p.bf.Has(idx)
}

func (p *Peer) sendHave(pieceIdx int) {
	if p.closed.Load() {
		return
	}

	p.outq <- MessageHave(pieceIdx)
}

func (p *Peer) requestNextPiece() {
	if p.closed.Load() {
		return
	}

	if p.shouldBeInterested() && !p.amInterested {
		p.sendInterested()
		return
	}
	if p.peerChoking {
		return
	}

	pv := p.piecePeerView()
	if !pv.Unchoked {
		return
	}

	reqs := p.m.pieceManager.NextForPeer(pv)
	if len(reqs) == 0 {
		return
	}
	for _, req := range reqs {
		p.outq <- MessageRequest(req.Piece, req.Begin, req.Length)
	}

	p.statsMu.Lock()
	p.requestsSent += len(reqs)
	p.statsMu.Unlock()
}

func (p *Peer) piecePeerView() *piece.PeerView {
	return &piece.PeerView{
		Peer:     p.addr,
		Has:      p.bf,
		Unchoked: !p.peerChoking,
	}
}
