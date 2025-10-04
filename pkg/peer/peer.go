package peer

import (
	"context"
	"encoding/hex"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/prxssh/rabbit/pkg/piece"
	"github.com/prxssh/rabbit/pkg/storage"
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
	statsMut        sync.RWMutex
	connectedAt     time.Time
	downloadedBytes int64
	uploadedBytes   int64
	requestsSent    int
	blocksReceived  int
	blocksFailed    int
	lastActiveAt    time.Time
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
		Timeout:   m.cfg.DialTimeout,
		KeepAlive: m.cfg.DialTimeout,
		Control:   nil,
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr.String())
	if err != nil {
		return nil, err
	}

	l := slog.Default().With(
		"src", "peer",
		"remote", conn.RemoteAddr().String(),
		"local", conn.LocalAddr().String(),
		"info_hash", hex.EncodeToString(m.infoHash[:]),
	)

	l.Info("connected")

	_ = conn.SetReadDeadline(time.Now().Add(m.cfg.ReadTimeout))
	_ = conn.SetWriteDeadline(time.Now().Add(m.cfg.WriteTimeout))

	hs := NewHandshake(m.infoHash, m.clientID)
	if err := hs.Perform(conn); err != nil {
		l.Warn("handshake failed", slog.String("err", err.Error()))

		_ = conn.Close()
		return nil, err
	}

	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})

	l.Info("handshake ok")

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
			m.cfg.PeerOutboundQueueBacklog,
		),
	}, nil
}

// Stats returns a snapshot of this peer's current performance metrics.
func (p *Peer) Stats() PeerStats {
	p.statsMut.RLock()
	defer p.statsMut.RUnlock()

	connectedFor := time.Since(p.connectedAt)

	var downloadRate int64
	if connectedFor > 0 {
		downloadRate = p.downloadedBytes / int64(connectedFor.Seconds())
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
		IsInterested:   p.amInterested,
	}
}

// run executes the peer's read and write loops until the context is cancelled
// or an error occurs. This method blocks until the peer stops.
func (p *Peer) run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return p.readLoop(ctx) })
	eg.Go(func() error { return p.writeLoop(ctx) })

	err := eg.Wait()

	if cleanupErr := p.cleanup(); cleanupErr != nil && err == nil {
		err = cleanupErr
	}

	return err
}

func (p *Peer) cleanup() error {
	close(p.outq)
	return p.conn.Close()
}

// readLoop continuously reads and processes messages from the peer until
// the context is cancelled or an error occurs.
func (p *Peer) readLoop(ctx context.Context) error {
	l := p.log.With("src", "read.loop")
	l.Info("start read loop")

	lastRecv := time.Now()

	for {
		select {
		case <-ctx.Done():
			l.Info(
				"loop exit",
				slog.String("reason", "ctx"),
				slog.String("err", ctx.Err().Error()),
			)
			return ctx.Err()
		default:
		}

		msg, err := p.readMessage()
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			if time.Since(lastRecv) > 5*time.Minute {
				l.Warn(
					"idle timeout",
					slog.Duration(
						"idle",
						time.Since(lastRecv),
					),
				)

				return context.DeadlineExceeded
			}
			continue
		}
		if err != nil {
			l.Warn("read error", slog.String("err", err.Error()))

			p.m.picker.OnPeerGone(p.addr, p.bf)
			return err
		}

		if msg == nil { // keep-alive
			l.Debug("received keep-alive")
			lastRecv = time.Now()
			continue
		}

		lastRecv = time.Now()

		p.statsMut.Lock()
		p.lastActiveAt = lastRecv
		p.statsMut.Unlock()

		switch msg.ID {
		case MsgChoke:
			l.Debug(
				"message",
				slog.String("message", MsgChoke.String()),
			)
			p.peerChoking = true

		case MsgUnchoke:
			l.Debug(
				"message",
				slog.String("message", MsgUnchoke.String()),
			)

			p.peerChoking = false
			p.requestNextPiece()

		case MsgInterested:
			l.Debug(
				"message",
				slog.String("message", MsgInterested.String()),
			)

			p.peerInterested = true

		case MsgNotInterested:
			l.Debug(
				"message",
				slog.String(
					"message",
					MsgNotInterested.String(),
				),
			)

			p.peerInterested = false

		case MsgBitfield:
			l.Debug(
				"message",
				slog.String("message", MsgBitfield.String()),
			)

			p.bf = bitfield.FromBytes(msg.Payload)
			p.m.picker.OnPeerBitfield(p.addr, p.bf)

			if p.shouldBeInterested() && !p.amInterested {
				p.sendInterested()
			}
			p.requestNextPiece()

		case MsgHave:
			pieceIdx, ok := msg.ParseHave()
			if !ok {
				continue
			}

			l.Debug(
				"message",
				slog.String("message", MsgHave.String()),
				slog.Uint64("piece_index", uint64(pieceIdx)),
			)

			p.bf.Set(int(pieceIdx))
			p.m.picker.OnPeerHave(p.addr, int(pieceIdx))

			if p.shouldBeInterested() && !p.amInterested {
				p.sendInterested()
			}
			p.requestNextPiece()

		case MsgPiece:
			idx, begin, data, ok := msg.ParsePiece()
			if !ok {
				p.statsMut.Lock()
				p.blocksFailed++
				p.statsMut.Unlock()
				continue
			}

			l.Debug(
				"message",
				slog.String("message", MsgPiece.String()),
				slog.Uint64("index", uint64(idx)),
				slog.Uint64("begin", uint64(begin)),
			)

			p.statsMut.Lock()
			p.blocksReceived++
			p.downloadedBytes += int64(len(data))
			p.m.totalDownloaded += int64(len(data))
			p.statsMut.Unlock()

			off := int64(idx)*int64(p.m.pieceLength) + int64(begin)
			p.m.storage.Submit(
				storage.BlockWrite{Offset: off, Data: data},
			)

			pieceDone, _ := p.m.picker.OnBlockReceived(
				p.addr,
				int(idx),
				int(begin),
			)
			p.requestNextPiece()

			if !pieceDone {
				continue
			}

			pieceExactLen := p.m.pieceLength
			if int(idx) == p.m.picker.PieceCount-1 {
				pieceExactLen = int64(p.m.picker.LastPieceLen)
			}
			ok, err := p.m.storage.VerifyPiece(
				int(idx),
				int(p.m.pieceLength),
				int(pieceExactLen),
				p.m.picker.PieceHash(int(idx)),
			)
			if err != nil {
				l.Warn("verify piece failed", "err", err)
				ok = false
			}

			p.m.picker.MarkPieceVerified(ok)
			if !ok {
				l.Warn("piece hash mismatch", "piece", idx)
			}

			p.m.BroadcastHave(int(idx), p.addr)

			l.Info(
				"piece verified and broadcasted",
				slog.Int("piece", int(idx)),
			)

		case MsgRequest:
			l.Debug(
				"message",
				slog.String("message", MsgRequest.String()),
			)

		default:
			l.Warn(
				"message unknown",
				slog.Int("message", int(msg.ID)),
			)
		}

	}
}

// writeLoop continuously sends queued messages and keep-alives to the peer
// until the context is cancelled or an error occurs.
func (p *Peer) writeLoop(ctx context.Context) error {
	l := p.log.With("src", "write.loop")
	l.Info("start write loop")

	lastKeepAliveAt := time.Now().Add(-p.m.cfg.PeerHeartbeatInterval)
	keepAliveTicker := time.NewTicker(p.m.cfg.KeepAliveInterval)
	defer keepAliveTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Info(
				"exit",
				slog.String("reason", "ctx"),
				slog.String("err", ctx.Err().Error()),
			)
			return ctx.Err()

		case msg, ok := <-p.outq:
			if !ok {
				l.Info("outq closed")
				return nil
			}

			if err := p.writeMessage(msg); err != nil {
				l.Warn(
					"write failed",
					slog.String("err", err.Error()),
				)
				return err
			}

			l.Debug("msg sent", slog.Any("message", msg))

		case <-keepAliveTicker.C:
			if time.Since(
				lastKeepAliveAt,
			) < p.m.cfg.KeepAliveInterval {
				continue
			}
			if err := p.writeMessage(nil); err != nil {
				l.Warn(
					"keepalive send error",
					slog.String("err", err.Error()),
				)
				return err
			}

			lastKeepAliveAt = time.Now()
			l.Debug("peer keepalive sent")
		}
	}
}

func (p *Peer) writeMessage(message *Message) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(p.m.cfg.WriteTimeout))
	defer p.conn.SetWriteDeadline(time.Time{})

	return WriteMessage(p.conn, message)
}

func (p *Peer) readMessage() (*Message, error) {
	_ = p.conn.SetReadDeadline(time.Now().Add(p.m.cfg.ReadTimeout))
	defer p.conn.SetReadDeadline(time.Time{})

	return ReadMessage(p.conn)
}

func (p *Peer) sendInterested() {
	if p.amInterested {
		return
	}

	p.amInterested = true
	p.outq <- MessageInterested()
}

func (p *Peer) sendNotInterested() {
	if !p.amInterested {
		return
	}

	p.amInterested = false
	p.outq <- MessageNotInterested()
}

func (p *Peer) shouldBeInterested() bool {
	idx, ok := p.m.picker.CurrentPieceIndex()
	if !ok {
		return false
	}

	return p.bf.Has(idx)
}

func (p *Peer) requestNextPiece() {
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

	reqs := p.m.picker.NextForPeer(pv)
	if len(reqs) == 0 {
		return
	}
	for _, req := range reqs {
		p.outq <- MessageRequest(req.Piece, req.Begin, req.Length)
	}

	p.statsMut.Lock()
	p.requestsSent += len(reqs)
	p.statsMut.Unlock()
}

func (p *Peer) piecePeerView() *piece.PeerView {
	return &piece.PeerView{
		Peer:     p.addr,
		Has:      p.bf,
		Unchoked: !p.peerChoking,
	}
}
