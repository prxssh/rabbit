package dht

import (
	"crypto/sha1"
	"net"
)

type MessageType string

const (
	QueryType    MessageType = "q"
	ResponseType MessageType = "r"
	ErrorType    MessageType = "e"
)

type QueryMethod string

const (
	PingMethod         QueryMethod = "ping"
	FindNodeMethod     QueryMethod = "find_node"
	GetPeersMethod     QueryMethod = "get_peers"
	AnnouncePeerMethod QueryMethod = "announce_peer"
)

type Message struct {
	T string      // TransactionID
	Y MessageType // Message Type
	V string      // Client version

	Q QueryMethod    // Query method name
	A map[string]any // Query arguments

	R map[string]any // Response values

	E []any // Err [code, message]

	Addr *net.UDPAddr
}

func NewQuery(method QueryMethod, transactionID string) *Message {
	return &Message{
		T: transactionID,
		Y: QueryType,
		Q: method,
		A: make(map[string]any),
	}
}

func NewResponse(transactionID string) *Message {
	return &Message{
		T: transactionID,
		Y: ResponseType,
		R: make(map[string]any),
	}
}

func NewError(transactionID string, code int, message string) *Message {
	return &Message{
		T: transactionID,
		Y: ErrorType,
		E: []any{code, message},
	}
}

const (
	ErrorGeneric       = 201 // Generic Error
	ErrorServer        = 202 // Server Error
	ErrorProtocol      = 203 // Protocol Error
	ErrorMethodUnknown = 204 // Method Unknown
)

func PingQuery(transactionID string, senderID [sha1.Size]byte) *Message {
	msg := NewQuery(PingMethod, transactionID)
	msg.A["id"] = string(senderID[:])
	return msg
}

func PingResponse(transactionID string, senderID [sha1.Size]byte) *Message {
	msg := NewResponse(transactionID)
	msg.R["id"] = string(senderID[:])
	return msg
}

func FindNodeQuery(transactionID string, senderID, target [sha1.Size]byte) *Message {
	msg := NewQuery(FindNodeMethod, transactionID)
	msg.A["id"] = string(senderID[:])
	msg.A["target"] = string(target[:])
	return msg
}

func FindNodeResponse(transactionID string, senderID [sha1.Size]byte, nodes []byte) *Message {
	msg := NewResponse(transactionID)
	msg.R["id"] = string(senderID[:])
	msg.R["nodes"] = string(nodes)
	return msg
}

func GetPeersQuery(transactionID string, senderID, infoHash [sha1.Size]byte) *Message {
	msg := NewQuery(GetPeersMethod, transactionID)
	msg.A["id"] = string(senderID[:])
	msg.A["info_hash"] = string(infoHash[:])
	return msg
}

func GetPeersResponse(
	transactionID string,
	senderID [sha1.Size]byte,
	token string,
	values []string,
) *Message {
	msg := NewResponse(transactionID)
	msg.R["id"] = string(senderID[:])
	msg.R["token"] = token
	msg.R["values"] = values
	return msg
}

func GetPeersResponseNodes(
	transactionID string,
	senderID [sha1.Size]byte,
	token string,
	nodes []byte,
) *Message {
	msg := NewResponse(transactionID)
	msg.R["id"] = string(senderID[:])
	msg.R["token"] = token
	msg.R["nodes"] = string(nodes)
	return msg
}

func AnnouncePeerQuery(
	transactionID string,
	senderID, infoHash [sha1.Size]byte,
	port int,
	token string,
) *Message {
	msg := NewQuery(AnnouncePeerMethod, transactionID)
	msg.A["id"] = string(senderID[:])
	msg.A["info_hash"] = string(infoHash[:])
	msg.A["port"] = port
	msg.A["token"] = token
	return msg
}

func AnnouncePeerResponse(transactionID string, senderID [sha1.Size]byte) *Message {
	msg := NewResponse(transactionID)
	msg.R["id"] = string(senderID[:])
	return msg
}

func (m *Message) GetNodeID() ([sha1.Size]byte, bool) {
	var (
		id    [sha1.Size]byte
		idStr string
		ok    bool
	)

	if m.Y == ResponseType && m.R != nil {
		idStr, ok = m.R["id"].(string)
	} else if m.Y == QueryType && m.A != nil {
		idStr, ok = m.A["id"].(string)
	}

	if !ok || len(idStr) != sha1.Size {
		return id, false
	}

	copy(id[:], idStr)
	return id, true
}

func (m *Message) GetTarget() ([sha1.Size]byte, bool) {
	var target [sha1.Size]byte

	if m.Y != QueryType || m.A == nil {
		return target, false
	}

	targetStr, ok := m.A["target"].(string)
	if !ok || len(targetStr) != sha1.Size {
		return target, false
	}

	copy(target[:], targetStr)
	return target, true
}

func (m *Message) GetInfoHash() ([sha1.Size]byte, bool) {
	var hash [sha1.Size]byte

	if m.Y != QueryType || m.A == nil {
		return hash, false
	}

	hashStr, ok := m.A["info_hash"].(string)
	if !ok || len(hashStr) != sha1.Size {
		return hash, false
	}

	copy(hash[:], hashStr)
	return hash, true
}

func (m *Message) GetToken() (string, bool) {
	if m.Y == ResponseType && m.R != nil {
		token, ok := m.R["token"].(string)
		return token, ok
	}

	if m.Y == QueryType && m.A != nil {
		token, ok := m.A["token"].(string)
		return token, ok
	}

	return "", false
}

func (m *Message) GetNodes() ([]byte, bool) {
	if m.Y != ResponseType || m.R == nil {
		return nil, false
	}

	nodesStr, ok := m.R["nodes"].(string)
	if !ok {
		return nil, false
	}

	return []byte(nodesStr), true
}

func (m *Message) GetValues() ([]string, bool) {
	if m.Y != ResponseType || m.R == nil {
		return nil, false
	}

	valuesRaw, ok := m.R["values"].([]any)
	if !ok {
		return nil, false
	}

	values := make([]string, 0, len(valuesRaw))
	for _, v := range valuesRaw {
		if str, ok := v.(string); ok {
			values = append(values, str)
		}
	}

	return values, len(values) > 0
}

func (m *Message) GetPort() (int, bool) {
	if m.Y != QueryType || m.A == nil {
		return 0, false
	}

	port, ok := m.A["port"].(int)
	if !ok {
		if port64, ok := m.A["port"].(int64); ok {
			return int(port64), true
		}
		return 0, false
	}

	return port, true
}

func (m *Message) IsQuery() bool {
	return m.Y == QueryType
}

func (m *Message) IsResponse() bool {
	return m.Y == ResponseType
}

func (m *Message) IsError() bool {
	return m.Y == ErrorType
}
