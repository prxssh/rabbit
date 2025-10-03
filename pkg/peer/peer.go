package peer

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"time"

	"github.com/prxssh/rabbit/pkg/piece"
	"github.com/prxssh/rabbit/pkg/storage"
	"github.com/prxssh/rabbit/pkg/utils/bitfield"
	"golang.org/x/sync/errgroup"
)

// Peer represents one remote BitTorrent peer connection.
type Peer struct {
	m    *Manager
	log  *slog.Logger
	conn net.Conn
	addr netip.AddrPort

	// Our & their choke/interest state.
	amChoking      bool
	amInterested   bool
	peerChoking    bool
	peerInterested bool

	// Peer's advertised pieces.
	bf bitfield.Bitfield

	outq chan *Message

	// Lifecycle.
	grp     *errgroup.Group
	started bool
	cancel  context.CancelFunc
}

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
		bf:             bitfield.New(m.pieceCount),
		outq:           make(chan *Message, 50),
	}, nil
}

func (p *Peer) Start(ctx context.Context) {
	if p.started {
		p.log.Warn(
			"start ignored",
			slog.String("reason", "already started"),
		)
		return
	}
	p.started = true

	childCtx, cancel := context.WithCancel(ctx)
	g, gctx := errgroup.WithContext(childCtx)
	p.cancel = cancel
	p.grp = g

	p.log.Info("peer started")

	g.Go(func() error { return p.readLoop(gctx) })
	g.Go(func() error { return p.writeLoop(gctx) })
}

func (p *Peer) Stop() error {
	p.log.Info("stopping peer")

	if p.cancel != nil {
		p.cancel()
	}
	_ = p.conn.Close()

	var err error
	if p.grp != nil {
		err = p.grp.Wait()
		p.grp = nil
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		p.log.Warn("peer stopped end", slog.String("err", err.Error()))
		return err
	}

	p.log.Info("peer stopped")
	return nil
}

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

			p.m.picker.OnPeerGone(p.addr)
			return err
		}

		if msg == nil { // keep-alive
			l.Debug("received keep-alive")

			lastRecv = time.Now()
			continue
		}
		lastRecv = time.Now()

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
				slog.String(
					"message",
					MsgBitfield.String(),
				),
			)

			p.bf = bitfield.FromBytes(msg.Payload)
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
			if p.shouldBeInterested() && !p.amInterested {
				p.sendInterested()
			}
			p.requestNextPiece()

		case MsgPiece:
			idx, begin, data, ok := msg.ParsePiece()
			if !ok {
				continue
			}

			l.Debug(
				"message",
				slog.String("message", MsgPiece.String()),
				slog.Uint64("index", uint64(idx)),
				slog.Uint64("begin", uint64(begin)),
			)

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
			if int(idx) == p.m.picker.PieceCount {
				pieceExactLen = int64(p.m.picker.LastPieceLen)
			}
			ok, err := p.m.storage.VerifyPiece(
				int(idx),
				int(p.m.pieceLength),
				int(pieceExactLen),
				p.m.picker.PiceHash(int(idx)),
			)
			if err != nil {
				l.Warn("verify read failed", "err", err)
				ok = false
			}
			p.m.picker.MarkPieceVerified(ok)
			if !ok {
				l.Warn("piece hash mismatch", "piece", idx)
			}

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

func (p *Peer) writeLoop(ctx context.Context) error {
	l := p.log.With("src", "write.loop")
	l.Info("start write loop")

	lastKeepAliveAt := time.Now().Add(-p.m.cfg.KeepAliveInterval)
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

	for {
		pv := p.piecePeerView()
		if !pv.Unchoked {
			return
		}

		req := p.m.picker.NextForPeer(pv)
		if req == nil {
			return
		}

		p.outq <- MessageRequest(req.Piece, req.Begin, req.Length)
	}
}

func (p *Peer) piecePeerView() *piece.PeerView {
	return &piece.PeerView{
		Peer:     p.addr,
		Has:      p.bf,
		Unchoked: !p.peerChoking,
	}
}
