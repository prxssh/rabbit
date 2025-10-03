package tracker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
)

func decodePeers(v any, ipv6 bool) ([]netip.AddrPort, error) {
	switch t := v.(type) {
	case string:
		if ipv6 {
			return decodeCompactPeersV6([]byte(t))
		}
		return decodeCompactPeersV4([]byte(t))
	case []byte:
		if ipv6 {
			return decodeCompactPeersV6(t)
		}
		return decodeCompactPeersV4(t)
	case []any:
		return decodeDictPeers(t)
	default:
		return nil, fmt.Errorf("invalid peers type %T", v)
	}
}

func decodeCompactPeersV4(b []byte) ([]netip.AddrPort, error) {
	if len(b)%strideV4 != 0 {
		return nil, errors.New("peer length not multiple of 6")
	}

	n := len(b) / strideV4
	peers := make([]netip.AddrPort, n)

	for i, off := 0, 0; i < n; i, off = i+1, off+strideV4 {
		a := netip.AddrFrom4(
			[4]byte{b[off], b[off+1], b[off+2], b[off+3]},
		)
		p := binary.BigEndian.Uint16(b[off+4 : off+6])
		peers[i] = netip.AddrPortFrom(a, p)
	}

	return peers, nil
}

func decodeCompactPeersV6(b []byte) ([]netip.AddrPort, error) {
	if len(b)%strideV6 != 0 {
		return nil, errors.New("peer length not multiple of 18")
	}

	n := len(b) / strideV6
	peers := make([]netip.AddrPort, n)

	for i, off := 0, 0; i < n; i, off = i+1, off+strideV6 {
		var a16 [16]byte
		copy(a16[:], b[off:off+16])

		a := netip.AddrFrom16(a16)
		p := binary.BigEndian.Uint16(b[off+16 : off+18])
		peers[i] = netip.AddrPortFrom(a, p)
	}

	return peers, nil
}

func decodeDictPeers(list []any) ([]netip.AddrPort, error) {
	peers := make([]netip.AddrPort, 0, len(list))

	for i, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("peer[%d] not dict", i)
		}

		var addr netip.Addr

		switch ipv := m["ip"].(type) {
		case string:
			a, err := netip.ParseAddr(ipv)
			if err != nil {
				return nil, fmt.Errorf("peer[%d]: bad ip %q: %w", i, ipv, err)
			}
			addr = a
		case []byte:
			switch len(ipv) {
			case 4:
				addr = netip.AddrFrom4([4]byte{ipv[0], ipv[1], ipv[2], ipv[3]})
			case 16:
				var a16 [16]byte
				copy(a16[:], ipv)
				addr = netip.AddrFrom16(a16)
			default:
				return nil, fmt.Errorf("peer[%d]: bad ip bytes len=%d", i, len(ipv))
			}
		default:
			return nil, fmt.Errorf("peer[%d]: unsupported ip type %T", i, m["ip"])
		}

		p64, ok := m["port"].(int64)
		if !ok || p64 < 1 || p64 > 65535 {
			return nil, fmt.Errorf(
				"peer[%d]: invalid port %v",
				i,
				m["port"],
			)
		}

		peers = append(peers, netip.AddrPortFrom(addr, uint16(p64)))
	}

	return peers, nil
}
