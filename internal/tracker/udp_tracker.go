package tracker

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"sync"
	"time"
)

const (
	protocolID      = 0x41727101980
	baseBackoff     = 15 * time.Second
	connectionIDTTL = 60 * time.Second
	maxRetries      = 8
	maxUDPPacket    = 4096
)

const (
	actionConnect uint32 = iota
	actionAnnounce
	actionScrape
	actionError
)

var (
	errActionMismatch        = errors.New("action mismatch")
	errTransactionIDMismatch = errors.New("transaction id mismatch")
	errPacketTooShort        = errors.New("packet too short")
	errAttemptsExhausted     = errors.New("tracker: exhausted all attempts")
)

type UDPTracker struct {
	logger    *slog.Logger
	mut       sync.Mutex
	conn      *net.UDPConn
	key       uint32
	connID    uint64
	connIDTTL time.Time
	readBuf   []byte // reusable read buffer
}

func NewUDPTracker(url *url.URL, logger *slog.Logger) (*UDPTracker, error) {
	logger = logger.With("type", "udp")

	addr, err := net.ResolveUDPAddr("udp", url.Host)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}

	key, err := randU32()
	if err != nil {
		return nil, err
	}

	return &UDPTracker{
		conn:    conn,
		key:     key,
		logger:  logger,
		readBuf: make([]byte, maxUDPPacket),
	}, nil
}

func (ut *UDPTracker) Announce(
	ctx context.Context,
	params *AnnounceParams,
) (*AnnounceResponse, error) {
	ut.mut.Lock()
	defer ut.mut.Unlock()

	if time.Now().After(ut.connIDTTL) {
		if err := ut.performConnect(ctx); err != nil {
			return nil, err
		}
	}

	resp, err := ut.performAnnounce(ctx, params)
	if err == nil {
		return resp, nil
	}

	if errors.Is(err, errActionMismatch) || errors.Is(err, errTransactionIDMismatch) {
		ut.logger.Warn(
			"announce failed, connection ID may be stale, reconnecting...",
			"error", err,
		)
		ut.connIDTTL = time.Time{}

		if err := ut.performConnect(ctx); err != nil {
			return nil, err
		}

		return ut.performAnnounce(ctx, params)
	}

	return nil, err
}

func (ut *UDPTracker) performConnect(ctx context.Context) error {
	for n := 0; n < maxRetries; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		timeout, err := getTimeout(ctx, n)
		if err != nil {
			return err
		}
		_ = ut.conn.SetDeadline(time.Now().Add(timeout))

		transactionID, err := randU32()
		if err != nil {
			ut.logger.Warn("udp connect txid rand error", "error", err.Error())
			continue
		}

		if err := ut.sendConnectPacket(transactionID); err != nil {
			ut.logger.Warn("udp connect send error", "error", err.Error(), "retry", n)
			continue
		}

		connID, err := ut.readConnectPacket(transactionID)
		if err != nil {
			ut.logger.Warn("udp connect read error", "error", err.Error(), "retry", n)
			continue
		}

		ut.connID = connID
		ut.connIDTTL = time.Now().Add(connectionIDTTL)
		ut.logger.Debug("udp connect success", "connID", connID)

		return nil
	}

	return errAttemptsExhausted
}

func (ut *UDPTracker) performAnnounce(
	ctx context.Context,
	params *AnnounceParams,
) (*AnnounceResponse, error) {
	for n := 0; n < maxRetries; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		timeout, err := getTimeout(ctx, n)
		if err != nil {
			return nil, err
		}
		_ = ut.conn.SetDeadline(time.Now().Add(timeout))

		transactionID, err := randU32()
		if err != nil {
			ut.logger.Warn("udp announce txid rand error", "error", err.Error())
			continue
		}

		if err := ut.sendAnnouncePacket(transactionID, params); err != nil {
			ut.logger.Warn("udp announce send error", "error", err.Error(), "retry", n)
			continue
		}

		resp, err := ut.readAnnouncePacket(transactionID)
		if err != nil {
			if errors.Is(err, errActionMismatch) ||
				errors.Is(err, errTransactionIDMismatch) {
				ut.logger.Warn(
					"udp announce failed, connection ID stale",
					"error", err.Error(),
				)
				return nil, err
			}

			continue
		}

		return resp, nil
	}

	return nil, errAttemptsExhausted
}

func (ut *UDPTracker) sendConnectPacket(transactionID uint32) error {
	var packet [16]byte

	binary.BigEndian.PutUint64(packet[0:8], protocolID)
	binary.BigEndian.PutUint32(packet[8:12], actionConnect)
	binary.BigEndian.PutUint32(packet[12:16], transactionID)
	_, err := ut.conn.Write(packet[:])

	return err
}

func (ut *UDPTracker) readConnectPacket(transactionID uint32) (uint64, error) {
	var packet [16]byte

	nread, err := ut.conn.Read(packet[:])
	if err != nil {
		return 0, err
	}
	if nread < 16 {
		return 0, errPacketTooShort
	}

	action := binary.BigEndian.Uint32(packet[0:4])
	if action == actionError {
		return 0, fmt.Errorf("tracker error: %s", string(packet[8:nread]))
	}
	if action != actionConnect {
		return 0, errActionMismatch
	}

	receivedTransactionID := binary.BigEndian.Uint32(packet[4:8])
	if receivedTransactionID != transactionID {
		return 0, errTransactionIDMismatch
	}

	return binary.BigEndian.Uint64(packet[8:16]), nil
}

func (ut *UDPTracker) sendAnnouncePacket(transactionID uint32, params *AnnounceParams) error {
	var packet [98]byte

	binary.BigEndian.PutUint64(packet[0:8], ut.connID)
	binary.BigEndian.PutUint32(packet[8:12], actionAnnounce)
	binary.BigEndian.PutUint32(packet[12:16], transactionID)
	copy(packet[16:36], params.InfoHash[:])
	copy(packet[36:56], params.PeerID[:])
	binary.BigEndian.PutUint64(packet[56:64], params.Downloaded)
	binary.BigEndian.PutUint64(packet[64:72], params.Left)
	binary.BigEndian.PutUint64(packet[72:80], params.Uploaded)
	binary.BigEndian.PutUint32(packet[80:84], uint32(params.Event))
	binary.BigEndian.PutUint32(packet[84:88], 0)
	binary.BigEndian.PutUint32(packet[88:92], ut.key)
	binary.BigEndian.PutUint32(packet[92:96], params.numWant)
	binary.BigEndian.PutUint16(packet[96:98], params.port)

	_, err := ut.conn.Write(packet[:])
	return err
}

func (ut *UDPTracker) readAnnouncePacket(
	transactionID uint32,
) (*AnnounceResponse, error) {
	nread, err := ut.conn.Read(ut.readBuf)
	if err != nil {
		return nil, err
	}

	packet := ut.readBuf[:nread]
	if nread < 20 {
		return nil, errPacketTooShort
	}

	action := binary.BigEndian.Uint32(packet[0:4])
	if action == actionError {
		return nil, fmt.Errorf("tracker error: %s", string(packet[8:nread]))
	}
	if action != actionAnnounce {
		return nil, errActionMismatch
	}

	receivedTransactionID := binary.BigEndian.Uint32(packet[4:8])
	if receivedTransactionID != transactionID {
		return nil, errTransactionIDMismatch
	}

	interval := binary.BigEndian.Uint32(packet[8:12])
	leechers := binary.BigEndian.Uint32(packet[12:16])
	seeders := binary.BigEndian.Uint32(packet[16:20])

	peers, err := decodePeers(packet[20:], false)
	if err != nil {
		return nil, err
	}

	return &AnnounceResponse{
		Interval: time.Duration(interval) * time.Second,
		Leechers: int64(leechers),
		Seeders:  int64(seeders),
		Peers:    peers,
	}, nil
}

func randU32() (uint32, error) {
	var b [4]byte

	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func getTimeout(ctx context.Context, n int) (time.Duration, error) {
	timeout := baseBackoff * (1 << n)

	if deadline, ok := ctx.Deadline(); ok {
		remain := time.Until(deadline)
		if remain <= 0 {
			return 0, context.DeadlineExceeded
		}
		if remain < timeout {
			return remain, nil
		}
	}

	return timeout, nil
}
