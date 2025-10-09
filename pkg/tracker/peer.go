package tracker

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

const (
	strideV4 = 6  // 4 bytes IP + 2 bytes port
	strideV6 = 18 // 16 bytes IP + 2 bytes port
)

func decodePeers(v any, ipv6 bool) ([]netip.AddrPort, error) {
	switch t := v.(type) {
	case string:
		return decodeCompact([]byte(t), ipv6)
	case []byte:
		return decodeCompact(t, ipv6)
	case []any:
		return decodeDictPeers(t)
	default:
		return nil, fmt.Errorf("invalid peers type %T", v)
	}
}

func decodeCompact(data []byte, ipv6 bool) ([]netip.AddrPort, error) {
	if ipv6 {
		return decodeCompactPeers(data, strideV6, func(chunk []byte) netip.AddrPort {
			var a16 [16]byte
			copy(a16[:], chunk[:16])

			a := netip.AddrFrom16(a16)
			p := binary.BigEndian.Uint16(chunk[16:18])
			return netip.AddrPortFrom(a, p)
		})
	}

	return decodeCompactPeers(data, strideV4, func(chunk []byte) netip.AddrPort {
		a := netip.AddrFrom4([4]byte{chunk[0], chunk[1], chunk[2], chunk[3]})
		p := binary.BigEndian.Uint16(chunk[4:6])
		return netip.AddrPortFrom(a, p)
	})
}

func decodeCompactPeers(
	data []byte,
	stride int,
	decodeFunc func([]byte) netip.AddrPort,
) ([]netip.AddrPort, error) {
	if len(data)%stride != 0 {
		return nil, fmt.Errorf("malformed or invalid compact peers")
	}

	n := len(data) / stride
	out := make([]netip.AddrPort, n)
	for i, off := 0, 0; i < n; i, off = i+1, off+stride {
		out[i] = decodeFunc(data[off : off+stride])
	}

	return out, nil
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
			case strideV4:
				addr = netip.AddrFrom4([4]byte{ipv[0], ipv[1], ipv[2], ipv[3]})
			case strideV6:
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
			return nil, fmt.Errorf("peer[%d]: invalid port %v", i, m["port"])
		}

		peers = append(peers, netip.AddrPortFrom(addr, uint16(p64)))
	}

	return peers, nil
}
