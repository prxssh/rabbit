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

	"github.com/prxssh/rabbit/pkg/config"
	"github.com/prxssh/rabbit/pkg/piece"
	"github.com/prxssh/rabbit/pkg/utils/bitfield"
	"golang.org/x/sync/errgroup"
)

type Hooks struct {
	OnNeedWork        func(addr netip.AddrPort)
	OnHave            func(addr netip.AddrPort, piece uint32)
	OnBitfield        func(addr netip.AddrPort, bf bitfield.Bitfield)
	OnRequest         func(addr netip.AddrPort, piece, begin, length uint32) ([]byte, error)
	OnBlockReceived   func(addr netip.AddrPort, piece, begin uint32, data []byte)
	OnCheckInterested func(bf bitfield.Bitfield) bool
}

type PeerStats struct {
	DownloadedBytes atomic.Int64
	UploadedBytes   atomic.Int64
	RequestsSent    atomic.Int64
	BlocksReceived  atomic.Int64
	BlocksFailed    atomic.Int64
	LastActiveUnix  atomic.Int64
}

type Peer struct {
	log         *slog.Logger
	conn        net.Conn
	addr        netip.AddrPort
	connectedAt time.Time
	state       uint32
	closed      atomic.Bool
	stats       *PeerStats
	outQ        chan *Message
	piecesBF    bitfield.Bitfield
	piecesBFMu  sync.RWMutex
	hooks       Hooks
	pieceReqQ   chan *piece.Request
}

type PeerMetrics struct {
	Addr           netip.AddrPort
	Downloaded     int64
	Uploaded       int64
	RequestsSent   int
	BlocksReceived int
	BlocksFailed   int
	LastActive     time.Time
	ConnectedAt    time.Time
	ConnectedFor   time.Duration
	DownloadRate   int64
	UploadRate     int64
	IsChoked       bool
	IsInterested   bool
}

const (
	stateAmChoking      = 1 << 0
	stateAmInterested   = 1 << 1
	statePeerChoking    = 1 << 2
	statePeerInterested = 1 << 3
)

type PeerOpts struct {
	Pieces    int
	PieceReqQ <-chan piece.Request
	PiecesBF  bitfield.Bitfield
	InfoHash  [sha1.Size]byte
	ClientID  [sha1.Size]byte
	Log       *slog.Logger
	Hooks     Hooks
}

func NewPeer(ctx context.Context, addr netip.AddrPort, opts *PeerOpts) (*Peer, error) {
	log := opts.Log.With("src", "peer", "addr", addr.String())

	conn, err := net.DialTimeout("tcp", addr.String(), config.Load().DialTimeout)
	if err != nil {
		return nil, err
	}

	_ = conn.SetReadDeadline(time.Now().Add(config.Load().ReadTimeout))
	_ = conn.SetWriteDeadline(time.Now().Add(config.Load().WriteTimeout))

	h := NewHandshake(opts.InfoHash, opts.ClientID)
	if err := h.Perform(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	if err := WriteMessage(conn, MessageBitfield(opts.PiecesBF)); err != nil {
		_ = conn.Close()
		return nil, err
	}

	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})

	now := time.Now()

	p := &Peer{
		log:         log,
		conn:        conn,
		addr:        addr,
		connectedAt: now,
		stats:       &PeerStats{},
		piecesBF:    bitfield.New(opts.Pieces),
		pieceReqQ:   make(chan *piece.Request, 50),
		outQ:        make(chan *Message, config.Load().PeerOutboundQueueBacklog),
		hooks:       opts.Hooks,
	}

	if !p.areHooksValid() {
		return nil, errors.New("hooks is required and should be non-nil")
	}

	atomic.StoreUint32(&p.state, stateAmChoking|statePeerChoking)
	p.stats.LastActiveUnix.Store(now.UnixNano())

	log.Debug("peer connected")

	return p, nil
}

func (p *Peer) areHooksValid() bool {
	return p.hooks.OnNeedWork != nil &&
		p.hooks.OnBlockReceived != nil &&
		p.hooks.OnHave != nil &&
		p.hooks.OnRequest != nil &&
		p.hooks.OnBitfield != nil &&
		p.hooks.OnCheckInterested != nil
}

func (p *Peer) Run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error { return p.readLoop(ctx) })
	eg.Go(func() error { return p.writeLoop(ctx) })
	eg.Go(func() error { return p.pieceRequestLoop(ctx) })

	err := eg.Wait()
	p.cleanup()
	return err
}

func (p *Peer) Metrics() PeerMetrics {
	connectedFor := time.Since(p.connectedAt)
	sec := int64(connectedFor.Seconds())

	var downloadRate, uploadRate int64
	if sec > 0 {
		downloadRate = p.stats.DownloadedBytes.Load() / sec
		uploadRate = p.stats.UploadedBytes.Load() / sec
	}

	return PeerMetrics{
		Addr:           p.addr,
		DownloadRate:   downloadRate,
		UploadRate:     uploadRate,
		ConnectedAt:    p.connectedAt,
		ConnectedFor:   connectedFor,
		IsChoked:       p.peerChoking(),
		IsInterested:   p.amInterested(),
		Downloaded:     p.stats.DownloadedBytes.Load(),
		Uploaded:       p.stats.UploadedBytes.Load(),
		RequestsSent:   int(p.stats.RequestsSent.Load()),
		BlocksReceived: int(p.stats.BlocksReceived.Load()),
		BlocksFailed:   int(p.stats.BlocksFailed.Load()),
		LastActive:     time.Unix(0, p.stats.LastActiveUnix.Load()),
	}
}

func (p *Peer) cleanup() {
	if !p.closed.CompareAndSwap(false, true) {
		return
	}

	close(p.outQ)
	p.conn.Close()
}

func (p *Peer) setState(mask uint32, on bool) {
	for {
		old := atomic.LoadUint32(&p.state)
		var next uint32
		if on {
			next = old | mask
		} else {
			next = old &^ mask
		}

		if atomic.CompareAndSwapUint32(&p.state, old, next) {
			return
		}
	}
}

func (p *Peer) getState(mask uint32) bool {
	return atomic.LoadUint32(&p.state)&mask != 0
}

func (p *Peer) amChoking() bool     { return p.getState(stateAmChoking) }
func (p *Peer) amInterested() bool  { return p.getState(stateAmInterested) }
func (p *Peer) peerChoking() bool   { return p.getState(statePeerChoking) }
func (p *Peer) peerIntersted() bool { return p.getState(statePeerInterested) }

func (p *Peer) readLoop(ctx context.Context) error {
	l := p.log.With("src", "peer.read.loop")
	l.Info("started")

	lastReceived := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		message, err := p.readMessage()
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			if time.Since(lastReceived) > config.Load().PeerHeartbeatInterval {
				return context.DeadlineExceeded
			}
			continue
		}
		if err != nil {
			return err
		}

		lastReceived = time.Now()

		if message == nil {
			continue
		}

		switch message.ID {
		case MsgChoke:
			p.setState(statePeerChoking, true)
		case MsgUnchoke:
			p.setState(statePeerChoking, false)
			p.hooks.OnNeedWork(p.addr)
		case MsgInterested:
			p.setState(statePeerInterested, true)
			p.hooks.OnNeedWork(p.addr)
		case MsgNotInterested:
			p.setState(statePeerInterested, false)
		case MsgBitfield:
			bf := bitfield.FromBytes(message.Payload)
			p.piecesBFMu.Lock()
			p.piecesBF = bf
			p.piecesBFMu.Unlock()

			p.hooks.OnBitfield(p.addr, bf)
			if p.hooks.OnCheckInterested(bf) {
				p.sendInterested()
			}
			p.hooks.OnNeedWork(p.addr)
		case MsgHave:
			pieceIdx, ok := message.ParseHave()
			if !ok {
				continue
			}

			p.piecesBFMu.Lock()
			p.piecesBF.Set(int(pieceIdx))
			// Make copy while holding lock
			bf := make(bitfield.Bitfield, len(p.piecesBF))
			copy(bf, p.piecesBF)
			p.piecesBFMu.Unlock()

			p.hooks.OnHave(p.addr, pieceIdx)
			if p.hooks.OnCheckInterested(bf) {
				p.sendInterested()
			}
			p.hooks.OnNeedWork(p.addr)
		case MsgPiece:
			piece, begin, data, ok := message.ParsePiece()
			if !ok {
				p.stats.BlocksFailed.Add(1)
				continue
			}

			p.stats.BlocksReceived.Add(1)
			p.stats.DownloadedBytes.Add(int64(len(data)))

			p.hooks.OnBlockReceived(p.addr, piece, begin, data)
			p.hooks.OnNeedWork(p.addr)
		case MsgRequest:
			idx, begin, length, ok := message.ParseRequest()
			if !ok {
				continue
			}

			data, err := p.hooks.OnRequest(p.addr, idx, begin, length)
			if err != nil {
				l.Error("failed to fetch request block",
					"error", err,
					"piece", idx,
					"begin", begin,
					"length", length,
				)
				continue
			}

			l.Info("seeded block to peer",
				"piece", idx,
				"begin", begin,
				"length", length,
			)

			p.outQ <- MessagePiece(int(idx), int(begin), data)
			p.stats.UploadedBytes.Add(int64(len(data)))

		default:
			l.Warn("invalid message received", "message", message)
		}
	}
}

func (p *Peer) writeLoop(ctx context.Context) error {
	l := p.log.With("src", "peer.write.loop")
	l.Info("started")

	keepAliveTicker := time.NewTicker(config.Load().KeepAliveInterval)
	defer keepAliveTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case message, ok := <-p.outQ:
			if !ok {
				l.Warn("outq closed; exiting loop")
				return nil
			}

			if err := p.writeMessage(message); err != nil {
				l.Warn("write message failed; exiting loop", "error", err)
				return err
			}

		case <-keepAliveTicker.C:
			if err := p.writeMessage(nil); err != nil {
				l.Warn("write keepalive failed; exiting loop", "error", err)
				return err
			}
		}
	}
}

func (p *Peer) pieceRequestLoop(ctx context.Context) error {
	l := p.log.With("src", "peer.piece_request.loop")
	l.Info("started")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case req, ok := <-p.pieceReqQ:
			if !ok {
				l.Warn("pieceReqQ closed; exiting loop")
				return nil
			}

			p.outQ <- MessageRequest(req.Piece, req.Begin, req.Length)
			p.stats.RequestsSent.Add(1)
		}
	}
}

func (p *Peer) sendInterested() {
	if p.closed.Load() {
		return
	}

	if p.amInterested() {
		return
	}

	p.setState(stateAmInterested, true)
	p.outQ <- MessageInterested()
}

func (p *Peer) sendNotInterested() {
	if p.closed.Load() {
		return
	}

	if !p.amInterested() {
		return
	}

	p.setState(stateAmInterested, false)
	p.outQ <- MessageNotInterested()
}

func (p *Peer) sendHave(piece int) {
	if p.closed.Load() {
		return
	}

	p.outQ <- MessageHave(piece)
}

func (p *Peer) sendCancel(piece, begin, length int) {
	if p.closed.Load() {
		return
	}

	p.outQ <- MessageCancel(piece, begin, length)
}

func (p *Peer) Bitfield() bitfield.Bitfield {
	p.piecesBFMu.RLock()
	defer p.piecesBFMu.RUnlock()

	bf := make(bitfield.Bitfield, len(p.piecesBF))
	copy(bf, p.piecesBF)
	return bf
}

func (p *Peer) writeMessage(message *Message) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(config.Load().WriteTimeout))
	defer p.conn.SetWriteDeadline(time.Time{})

	err := WriteMessage(p.conn, message)
	if err == nil {
		p.stats.LastActiveUnix.Store(time.Now().UnixNano())
	}
	return err
}

func (p *Peer) readMessage() (*Message, error) {
	_ = p.conn.SetReadDeadline(time.Now().Add(config.Load().ReadTimeout))
	defer p.conn.SetReadDeadline(time.Time{})

	message, err := ReadMessage(p.conn)
	if err == nil {
		p.stats.LastActiveUnix.Store(time.Now().UnixNano())
	}
	return message, err
}

func (p *Peer) formatMessageDetails(msg *Message) string {
	if msg == nil {
		return "keep-alive"
	}

	switch msg.ID {
	case MsgHave:
		if piece, ok := msg.ParseHave(); ok {
			return fmt.Sprintf("#%d", piece)
		}
	case MsgRequest:
		if idx, begin, length, ok := msg.ParseRequest(); ok {
			return fmt.Sprintf("#%d @%d %dKB", idx, begin, length/1024)
		}
	case MsgPiece:
		if idx, begin, block, ok := msg.ParsePiece(); ok {
			return fmt.Sprintf("#%d @%d %dKB", idx, begin, len(block)/1024)
		}
	case MsgCancel:
		if idx, begin, length, ok := msg.ParseRequest(); ok {
			return fmt.Sprintf("#%d @%d %dKB", idx, begin, length/1024)
		}
	case MsgBitfield:
		return fmt.Sprintf("%d bytes", len(msg.Payload))
	}

	return ""
}
