package dht

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"net"
)

type Node struct {
	ID   [sha1.Size]byte
	IP   net.IP
	Port int
}

func NewNode(ip net.IP, port int16) *Node {
	return &Node{
		ID:   randNodeID(),
		IP:   ip,
		Port: int(port),
	}
}

func NewNodeWithID(id [sha1.Size]byte, ip net.IP, port int16) *Node {
	return &Node{
		ID:   id,
		IP:   ip,
		Port: int(port),
	}
}

func (n *Node) CompactNodeInfo() []byte {
	if ip4 := n.IP.To4(); ip4 != nil {
		buf := make([]byte, 26)
		copy(buf[:20], n.ID[:])
		copy(buf[20:24], ip4)
		binary.BigEndian.PutUint16(buf[24:26], uint16(n.Port))
		return buf
	}

	return nil
}

func DecodeCompactNodeInfo(data []byte) *Node {
	if len(data) != 26 {
		return nil
	}

	var id [sha1.Size]byte
	copy(id[:], data[:20])

	ip := net.IPv4(data[20], data[21], data[22], data[23])
	port := binary.BigEndian.Uint16(data[24:26])

	return &Node{
		ID:   id,
		IP:   ip,
		Port: int(port),
	}
}

func DecodeCompactNodeInfoList(data []byte) []*Node {
	if len(data)%26 != 0 {
		return nil
	}

	count := len(data) / 26
	nodes := make([]*Node, 0, count)

	for i := 0; i < count; i++ {
		offset := i * 26
		if node := DecodeCompactNodeInfo(data[offset : offset+26]); node != nil {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

func (n *Node) CompactNodeInfo6() []byte {
	if ip6 := n.IP.To16(); ip6 != nil && n.IP.To4() == nil {
		buf := make([]byte, 28)
		copy(buf[:20], n.ID[:])
		copy(buf[20:36], ip6)
		binary.BigEndian.PutUint16(buf[36:38], uint16(n.Port))
		return buf
	}

	return nil
}

func DecodeCompactNodeInfo6(data []byte) *Node {
	if len(data) != 38 {
		return nil
	}

	var id [sha1.Size]byte
	copy(id[:], data[:20])

	ip := make(net.IP, 16)
	copy(ip, data[20:36])
	port := binary.BigEndian.Uint16(data[36:38])

	return &Node{
		ID:   id,
		IP:   ip,
		Port: int(port),
	}
}

func DecodeCompactNodeInfo6List(data []byte) []*Node {
	if len(data)%38 != 0 {
		return nil
	}

	count := len(data) / 38
	nodes := make([]*Node, 0, count)

	for i := 0; i < count; i++ {
		offset := i * 38
		if node := DecodeCompactNodeInfo6(data[offset : offset+38]); node != nil {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

func (n *Node) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{
		IP:   n.IP,
		Port: int(n.Port),
	}
}

func (n *Node) String() string {
	return net.JoinHostPort(n.IP.String(), string(rune(n.Port)))
}

func randNodeID() [sha1.Size]byte {
	var nodeID [sha1.Size]byte

	if _, err := rand.Read(nodeID[:]); err != nil {
		panic("crypto/rand failure: " + err.Error())
	}
	return nodeID
}
