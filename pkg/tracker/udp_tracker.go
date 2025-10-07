package tracker

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"time"

	"github.com/prxssh/rabbit/pkg/config"
)

const (
	protocolID      = 0x41727101980
	baseBackoff     = 15 * time.Second
	connectionIDTTL = 60 * time.Second
	maxRetries      = 8
	maxUDPPacket    = 2048
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
)

type UDPTracker struct {
	conn      *net.UDPConn
	key       uint32
	connID    uint64
	connIDTTL time.Time
	log       *slog.Logger
}

func NewUDPTracker(url *url.URL, log *slog.Logger) (*UDPTracker, error) {
	if log == nil {
		log = slog.Default()
	}

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
		conn: conn,
		key:  key,
		log:  log,
	}, nil
}

func (ut *UDPTracker) Announce(
	ctx context.Context,
	params *AnnounceParams,
) (*AnnounceResponse, error) {
	deadline, hasDeadline := ctx.Deadline()

	for n := 0; n < maxRetries; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		timeout := backoffWindow(deadline, hasDeadline, n)
		if timeout <= 0 {
			return nil, context.DeadlineExceeded
		}
		_ = ut.conn.SetDeadline(time.Now().Add(timeout))

		if time.Now().After(ut.connIDTTL) {
			transactionID, err := randU32()
			if err != nil {
				ut.log.Warn("udp connect txid rand error",
					"error", err.Error(),
				)
				continue
			}

			if err := ut.sendConnectPacket(transactionID); err != nil {
				ut.log.Warn("udp connect send error",
					"error", err.Error(),
				)
				continue
			}

			connID, err := ut.readConnectPacket(transactionID)
			if err != nil {
				ut.log.Warn("udp connect read error",
					"err", err.Error(),
				)
				continue
			}
			ut.connID = connID
			ut.connIDTTL = time.Now().Add(connectionIDTTL)
		}

		transactionID, err := randU32()
		if err != nil {
			ut.log.Warn("udp announce txid rand error",
				"error", err.Error(),
			)
			continue
		}

		start := time.Now()
		if err := ut.sendAnnouncePacket(
			transactionID,
			ut.connID,
			params,
		); err != nil {
			ut.log.Warn("udp announce send error",
				"error", err.Error(),
			)
			continue
		}

		resp, err := ut.readAnnouncePacket(transactionID)
		lat := time.Since(start)
		if err != nil {
			if errors.Is(err, errActionMismatch) ||
				errors.Is(err, errTransactionIDMismatch) {
				ut.connIDTTL = time.Time{}

				ut.log.Warn("udp announce protocol mismatch",
					"error", err.Error(),
					"retry", n+1,
				)
			} else {
				ut.log.Warn("udp announce read error",
					"latency", lat,
					"err", err.Error(),
					"retry", n+1,
				)
			}
			continue
		}

		return resp, nil
	}

	return nil, errors.New("tracker: exhausted all announce attempts")
}

func (ut *UDPTracker) sendConnectPacket(transactionID uint32) error {
	var packet [16]byte
	binary.BigEndian.PutUint64(packet[0:8], protocolID)
	binary.BigEndian.PutUint32(packet[8:12], actionConnect)
	binary.BigEndian.PutUint32(packet[12:16], transactionID)

	if _, err := ut.conn.Write(packet[:]); err != nil {
		return err
	}

	return nil
}

func (ut *UDPTracker) readConnectPacket(
	transactionID uint32,
) (uint64, error) {
	var packet [16]byte

	nread, err := ut.conn.Read(packet[:])
	if err != nil {
		return 0, err
	}
	if nread < 16 {
		return 0, errors.New("small packet size")
	}

	action := binary.BigEndian.Uint32(packet[0:4])
	if action == actionError {
		return 0, errors.New(string(packet[8:nread]))
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

func (ut *UDPTracker) sendAnnouncePacket(
	transactionID uint32,
	connectionID uint64,
	params *AnnounceParams,
) error {
	var packet [98]byte

	binary.BigEndian.PutUint64(packet[0:8], connectionID)
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
	binary.BigEndian.PutUint32(packet[92:96], params.NumWant)
	binary.BigEndian.PutUint16(packet[96:98], params.Port)

	n, err := ut.conn.Write(packet[:])
	if err != nil {
		return err
	}

	ut.log.Debug("udp announce send",
		"bytes", n,
		"txid", uint64(transactionID),
		"conn_id", connectionID,
		"uploaded", params.Uploaded,
		"downloaded", params.Downloaded,
		"left", params.Left,
		"event", params.Event.String(),
	)

	return nil
}

func (ut *UDPTracker) readAnnouncePacket(
	transactionID uint32,
) (*AnnounceResponse, error) {
	packet := make([]byte, maxUDPPacket)
	nread, err := ut.conn.Read(packet)
	if err != nil {
		return nil, err
	}
	if nread < 20 {
		return nil, errors.New("announce resp too short")
	}

	action := binary.BigEndian.Uint32(packet[0:4])
	if action == actionError {
		return nil, errors.New(string(packet[8:nread]))
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

	peers, err := decodePeers(packet[20:nread], config.Load().HasIPV6)
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

func backoffWindow(deadline time.Time, hasDeadline bool, n int) time.Duration {
	d := baseBackoff << n
	if !hasDeadline {
		return d
	}

	remain := time.Until(deadline)
	if remain <= 0 {
		return 0
	}
	if remain < d {
		return remain
	}
	return d
}
