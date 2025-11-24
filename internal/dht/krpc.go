package dht

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/prxssh/rabbit/internal/bencode"
)

var (
	ErrTimeout       = errors.New("query timeout")
	ErrInvalidMsg    = errors.New("invalid message")
	ErrTransactionID = errors.New("unknown transaction id")
)

type KRPC struct {
	logger  *slog.Logger
	conn    *net.UDPConn
	localID [sha1.Size]byte

	txMut        sync.RWMutex
	transactions map[string]*transaction

	queryHandler    func(*Message)
	responseHandler func(*Message)

	done chan struct{}
	wg   sync.WaitGroup
}

type transaction struct {
	query      *Message
	responseCh chan *Message
	sentTime   time.Time
	timeout    time.Duration
	retries    int
}

func NewKRPC(localID [sha1.Size]byte, listenAddr string, logger *slog.Logger) (*KRPC, error) {
	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	return &KRPC{
		logger:       logger,
		conn:         conn,
		localID:      localID,
		transactions: make(map[string]*transaction),
		done:         make(chan struct{}),
	}, nil
}

func (k *KRPC) LocalAddr() *net.UDPAddr {
	return k.conn.LocalAddr().(*net.UDPAddr)
}

func (k *KRPC) Start() {
	k.wg.Go(func() { k.readLoop() })
	k.wg.Go(func() { k.timeoutLoop() })
}

func (k *KRPC) Stop() {
	close(k.done)
	k.conn.Close()
	k.wg.Wait()
}

func (k *KRPC) SetQueryHandler(handler func(*Message)) {
	k.queryHandler = handler
}

func (k *KRPC) SetResponseHandler(handler func(*Message)) {
	k.responseHandler = handler
}

func (k *KRPC) SendQuery(msg *Message, addr *net.UDPAddr, timeout time.Duration) (*Message, error) {
	if msg.T == "" {
		msg.T = k.generateTransactionID()
	}

	tx := &transaction{
		query:      msg,
		responseCh: make(chan *Message, 1),
		sentTime:   time.Now(),
		timeout:    timeout,
		retries:    0,
	}

	k.txMut.Lock()
	k.transactions[msg.T] = tx
	k.txMut.Unlock()

	if err := k.send(msg, addr); err != nil {
		k.removeTransaction(msg.T)
		return nil, err
	}

	select {
	case response := <-tx.responseCh:
		k.removeTransaction(msg.T)
		return response, nil
	case <-time.After(timeout):
		k.removeTransaction(msg.T)
		return nil, ErrTimeout
	case <-k.done:
		k.removeTransaction(msg.T)
		return nil, errors.New("krpc stopped")
	}
}

func (k *KRPC) SendResponse(msg *Message, addr *net.UDPAddr) error {
	return k.send(msg, addr)
}

func (k *KRPC) SendError(transactionID string, code int, message string, addr *net.UDPAddr) error {
	msg := NewError(transactionID, code, message)
	return k.send(msg, addr)
}

func (k *KRPC) send(msg *Message, addr *net.UDPAddr) error {
	data := k.messageToMap(msg)

	encoded, err := bencode.Marshal(data)
	if err != nil {
		return err
	}

	_, err = k.conn.WriteToUDP(encoded, addr)
	return err
}

func (k *KRPC) readLoop() {
	buf := make([]byte, 65536)

	for {
		select {
		case <-k.done:
			return
		default:
		}

		k.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := k.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if !errors.Is(err, net.ErrClosed) {
				k.logger.Error("read udp packet failed", "error", err.Error())
			}
			continue
		}

		data, err := bencode.Unmarshal(buf[:n])
		if err != nil {
			k.logger.Debug("malformed message", "error", err.Error(), "from", addr)
			continue
		}

		msg := k.mapToMessage(data, addr)
		if msg == nil {
			continue
		}
		k.handleMessage(msg)
	}
}

func (k *KRPC) timeoutLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-k.done:
			return
		case <-ticker.C:
			k.checkTimeouts()
		}
	}
}

func (k *KRPC) checkTimeouts() {
	now := time.Now()

	k.txMut.Lock()
	defer k.txMut.Unlock()

	for txID, tx := range k.transactions {
		if now.Sub(tx.sentTime) > tx.timeout {
			close(tx.responseCh)
			delete(k.transactions, txID)
		}
	}
}

func (k *KRPC) handleMessage(msg *Message) {
	switch msg.Y {
	case QueryType:
		if k.queryHandler != nil {
			k.queryHandler(msg)
		}

	case ResponseType:
		k.handleResponse(msg)

	case ErrorType:
		k.handleError(msg)
	}
}

func (k *KRPC) handleResponse(msg *Message) {
	k.txMut.RLock()
	tx, exists := k.transactions[msg.T]
	k.txMut.RUnlock()

	if !exists {
		k.logger.Debug("Received response for unknown transaction", "from", msg.Addr)
		if k.responseHandler != nil {
			k.responseHandler(msg)
		}
		return
	}

	k.logger.Debug("Received response", "from", msg.Addr, "txid", msg.T)

	select {
	case tx.responseCh <- msg:
	default:
	}
}

func (k *KRPC) handleError(msg *Message) {
	k.txMut.RLock()
	tx, exists := k.transactions[msg.T]
	k.txMut.RUnlock()

	if exists {
		close(tx.responseCh)
	}
}

func (k *KRPC) removeTransaction(transactionID string) {
	k.txMut.Lock()
	delete(k.transactions, transactionID)
	k.txMut.Unlock()
}

func (k *KRPC) generateTransactionID() string {
	b := make([]byte, 2)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (k *KRPC) messageToMap(msg *Message) map[string]any {
	m := make(map[string]any)

	m["t"] = msg.T
	m["y"] = string(msg.Y)

	if msg.V != "" {
		m["v"] = msg.V
	}

	switch msg.Y {
	case QueryType:
		m["q"] = string(msg.Q)
		m["a"] = msg.A

	case ResponseType:
		m["r"] = msg.R

	case ErrorType:
		m["e"] = msg.E
	}

	return m
}

func (k *KRPC) mapToMessage(data any, addr *net.UDPAddr) *Message {
	dict, ok := data.(map[string]any)
	if !ok {
		return nil
	}

	msg := &Message{Addr: addr}

	if t, ok := dict["t"].(string); ok {
		msg.T = t
	} else {
		return nil
	}

	if y, ok := dict["y"].(string); ok {
		msg.Y = MessageType(y)
	} else {
		return nil
	}

	if v, ok := dict["v"].(string); ok {
		msg.V = v
	}

	switch msg.Y {
	case QueryType:
		if q, ok := dict["q"].(string); ok {
			msg.Q = QueryMethod(q)
		}
		if a, ok := dict["a"].(map[string]any); ok {
			msg.A = a
		}

	case ResponseType:
		if r, ok := dict["r"].(map[string]any); ok {
			msg.R = r
		}

	case ErrorType:
		if e, ok := dict["e"].([]any); ok {
			msg.E = e
		}
	}

	return msg
}
