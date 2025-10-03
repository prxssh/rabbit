package peer

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"time"

	"github.com/prxssh/rabbit/pkg/utils/bitfield"
	"golang.org/x/sync/errgroup"
)

// TODO: make it configurable
const (
	readTimeout       = 45 * time.Second
	writeTimeout      = 45 * time.Second
	keepAliveInterval = 2 * time.Minute
	outboundLen       = 64
)

type Peer struct {
	conn net.Conn
	log  *slog.Logger

	AmChoking      bool
	AmInterested   bool
	PeerChoking    bool
	PeerInterested bool
	BF             bitfield.Bitfield

	infoHash [sha1.Size]byte
	clientID [sha1.Size]byte

	outq    chan *Message
	grp     *errgroup.Group
	started bool
	cancel  context.CancelFunc
}

func Connect(
	ctx context.Context,
	addr netip.AddrPort,
	infoHash, clientID [sha1.Size]byte,
	pieceCount int,
) (*Peer, error) {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   nil,
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr.String())
	if err != nil {
		return nil, err
	}

	l := slog.Default().With(
		"remote", conn.RemoteAddr().String(),
		"local", conn.LocalAddr().String(),
		"info_hash", hex.EncodeToString(infoHash[:]),
		"client_id", hex.EncodeToString(clientID[:]),
	)

	l.Info("peer.connected")

	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))

	hs := NewHandshake(infoHash, clientID)
	if err := hs.Perform(conn); err != nil {
		l.Warn("peer.handshake.failed", slog.String("err", err.Error()))

		_ = conn.Close()
		return nil, err
	}

	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})

	l.Info("peer.handshake.ok")

	return &Peer{
		conn:           conn,
		log:            l,
		AmChoking:      true,
		AmInterested:   false,
		PeerChoking:    true,
		PeerInterested: false,
		infoHash:       infoHash,
		clientID:       clientID,
		BF:             bitfield.New(pieceCount),
		outq:           make(chan *Message, outboundLen),
	}, nil
}

func (p *Peer) Start(ctx context.Context) {
	if p.started {
		p.log.Warn(
			"peer.start.ignored",
			slog.String("reason", "already started"),
		)
		return
	}
	p.started = true

	childCtx, cancel := context.WithCancel(ctx)
	g, gctx := errgroup.WithContext(childCtx)

	p.cancel = cancel
	p.grp = g

	p.log.Info("peer.start")

	g.Go(func() error { return p.readLoop(gctx) })
	g.Go(func() error { return p.writeLoop(gctx) })
}

func (p *Peer) Stop() error {
	p.log.Info("peer.stop.begin")

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
		p.log.Warn("peer.stop.end", slog.String("err", err.Error()))
		return err
	}

	p.log.Info("peer.stop.end")

	return nil
}

func (p *Peer) SendInterested() {
	if p.AmInterested {
		return
	}

	p.AmInterested = true
	p.outq <- MessageInterested()
}

func (p *Peer) SendNotInterested() {
	if !p.AmInterested {
		return
	}

	p.AmInterested = false
	p.outq <- MessageNotInterested()
}

/*
func (p *Peer) SendRequest(b piece.Block) {
	p.outq <- MessageRequest(b.Piece, b.Offset, b.Len)
}

func (p *Peer) SendCancel(b piece.Block) {
	p.outq <- MessageCancel(b.Piece, b.Offset, b.Len)
}
*/

func (p *Peer) readLoop(ctx context.Context) error {
	l := p.log.With("loop", "read")
	l.Info("loop.start")

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
					"peer.idle.timeout",
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
			l.Warn(
				"peer.read.error",
				slog.String("err", err.Error()),
			)

			return err
		}

		if msg == nil { // keep-alive
			l.Debug("peer.keepalive.recv")

			lastRecv = time.Now()
			continue
		}

		lastRecv = time.Now()

		switch msg.ID {
		case MsgChoke:
			l.Debug(
				"peer.msg",
				slog.String("message", MsgChoke.String()),
			)

			p.PeerChoking = true

		case MsgUnchoke:
			l.Debug(
				"peer.msg",
				slog.String("message", MsgUnchoke.String()),
			)

			p.PeerChoking = false

		case MsgInterested:
			l.Debug(
				"peer.msg",
				slog.String("message", MsgInterested.String()),
			)

			p.PeerInterested = true

		case MsgNotInterested:
			l.Debug(
				"peer.msg",
				slog.String(
					"message",
					MsgNotInterested.String(),
				),
			)

			p.PeerInterested = false

		case MsgBitfield:
			p.BF = bitfield.FromBytes(msg.Payload)

			l.Debug(
				"peer.msg",
				slog.String(
					"message",
					MsgNotInterested.String(),
				),
				slog.String("payload", p.BF.String()),
			)

		case MsgHave:
			pieceIdx, ok := msg.ParseHave()
			if !ok {
				continue
			}

			l.Debug(
				"peer.msg",
				slog.String("message", MsgHave.String()),
				slog.Uint64("piece_index", uint64(pieceIdx)),
			)

			p.BF.Set(int(pieceIdx))

		case MsgPiece:
			idx, begin, _, ok := msg.ParsePiece()
			if !ok {
				continue
			}

			l.Debug(
				"peer.msg",
				slog.String("message", MsgPiece.String()),
				slog.Uint64("index", uint64(idx)),
				slog.Uint64("begin", uint64(begin)),
			)

		case MsgRequest:
			l.Debug(
				"peer.msg",
				slog.String("message", MsgRequest.String()),
			)

		default:
			l.Warn(
				"peer.msg.unknown",
				slog.Int("message", int(msg.ID)),
			)
		}

	}
}

func (p *Peer) writeLoop(ctx context.Context) error {
	l := p.log.With("loop", "write")
	l.Info("loop.start")

	lastKeepAliveAt := time.Now().Add(-keepAliveInterval)
	keepAliveTicker := time.NewTicker(keepAliveInterval)
	defer keepAliveTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			l.Info(
				"loop.exit",
				slog.String("reason", "ctx"),
				slog.String("err", ctx.Err().Error()),
			)
			return ctx.Err()

		case msg, ok := <-p.outq:
			if !ok {
				l.Info("outq.closed")
				return nil
			}

			if err := p.writeMessage(msg); err != nil {
				l.Warn(
					"peer.write.error",
					slog.String("err", err.Error()),
				)
				return err
			}

			l.Debug(
				"peer.msg.sent",
				slog.String("message", msg.ID.String()),
			)

		case <-keepAliveTicker.C:
			if time.Since(lastKeepAliveAt) < keepAliveInterval {
				continue
			}
			if err := p.writeMessage(nil); err != nil {
				l.Warn(
					"peer.keepalive.send.error",
					slog.String("err", err.Error()),
				)
				return err
			}

			lastKeepAliveAt = time.Now()
			l.Debug("peer.keepalive.sent")
		}
	}
}

func (p *Peer) writeMessage(message *Message) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	defer p.conn.SetWriteDeadline(time.Time{})

	return WriteMessage(p.conn, message)
}

func (p *Peer) readMessage() (*Message, error) {
	_ = p.conn.SetReadDeadline(time.Now().Add(readTimeout))
	defer p.conn.SetReadDeadline(time.Time{})

	return ReadMessage(p.conn)
}
