package piece

import (
	"math/rand/v2"
	"net/netip"

	"github.com/prxssh/rabbit/internal/config"
	"github.com/prxssh/rabbit/internal/utils/bitfield"
)

func (pk *Picker) NextForPeer(peer *PeerView, n int) []*Request {
	if !peer.Unchoked || n <= 0 {
		return nil
	}

	capacity := pk.peerCapacity(peer.Addr)
	if capacity == 0 {
		return nil
	}

	n = min(n, capacity)

	pk.mu.RLock()
	endgame := pk.endgame
	pk.mu.RUnlock()

	if endgame {
		return pk.selectEndgameBlocks(peer, n)
	}

	reqs := make([]*Request, 0, n)
	count := 0

	for i := 0; i < pk.PieceCount && count < n; i++ {
		pk.mu.RLock()
		p := pk.pieces[i]
		if p.verified || p.doneBlocks == 0 || !peer.Bitfield.Has(i) {
			pk.mu.RUnlock()
			continue
		}
		pk.mu.RUnlock()

		pk.mu.Lock()
		for count < n {
			if block, ok := pk.findAvailableBlock(p, peer.Addr); ok {
				reqs = append(reqs, pk.createRequest(peer.Addr, p, block))
				count++
			} else {
				break
			}
		}
		pk.mu.Unlock()
	}

	if count == n {
		return reqs
	}

	var pieceSelectionStrategy func(netip.AddrPort, bitfield.Bitfield, int) []*Request

	switch config.Load().PieceDownloadStrategy {
	case config.PieceDownloadStrategySequential:
		pieceSelectionStrategy = pk.selectSequential
	case config.PieceDownloadStrategyRandom:
		pieceSelectionStrategy = pk.selectRandom
	default:
		pieceSelectionStrategy = pk.selectRarestFirst
	}

	remaining := n - count
	strategyRequests := pieceSelectionStrategy(peer.Addr, peer.Bitfield, remaining)
	reqs = append(reqs, strategyRequests...)
	return reqs
}

func (pk *Picker) selectEndgameBlocks(peer *PeerView, n int) []*Request {
	if !peer.Unchoked || n <= 0 {
		return nil
	}

	pk.mu.Lock()
	defer pk.mu.Unlock()

	reqs := make([]*Request, n, 0)

	for _, p := range pk.pieces {
		if len(reqs) >= n {
			break
		}

		if p.verified || !peer.Bitfield.Has(p.index) {
			continue
		}

		for bi, blk := range p.blocks {
			if len(reqs) >= n {
				break
			}

			if blk.status == blockInflight {
				if blk.owner != nil && blk.owner.addr == peer.Addr {
					continue
				}

				reqs = append(reqs, pk.createRequest(peer.Addr, p, bi))
			}
		}
	}

	return reqs
}

func (pk *Picker) selectSequential(
	peer netip.AddrPort,
	peerBF bitfield.Bitfield,
	n int,
) []*Request {
	reqs := make([]*Request, 0, n)

	pk.mu.Lock()
	defer pk.mu.Unlock()

	for pk.nextPiece < pk.PieceCount && pk.pieces[pk.nextPiece].verified {
		pk.nextPiece++
		pk.nextBlock = 0
	}
	if pk.nextPiece >= pk.PieceCount {
		return reqs
	}

	if !peerBF.Has(pk.nextPiece) {
		return reqs
	}

	p := pk.pieces[pk.nextPiece]

	for bi := pk.nextBlock; bi < p.blockCount && len(reqs) < n; bi++ {
		if p.blocks[bi].status != blockWant ||
			pk.isBlockAssignedtoPeer(peer, p.index, bi*BlockLength) {
			continue
		}

		reqs = append(reqs, pk.createRequest(peer, p, bi))
		pk.nextBlock = bi + 1
	}

	return reqs
}

func (pk *Picker) selectRandom(peer netip.AddrPort, peerBF bitfield.Bitfield, n int) []*Request {
	available := make([]int, 0, pk.PieceCount)

	for i := 0; i < pk.PieceCount; i++ {
		pk.mu.RLock()
		if pk.isValidPiece(i) && peerBF.Has(i) {
			available = append(available, i)
		}
		pk.mu.RUnlock()
	}

	if len(available) == 0 {
		return nil
	}

	lenAvailable := len(available)
	n = min(n, lenAvailable)

	for i := 0; i < n; i++ {
		j := i + rand.IntN(lenAvailable-i)
		available[i], available[j] = available[j], available[i]
	}

	reqs := make([]*Request, 0, n)

	for i := 0; i < n; i++ {
		pk.mu.Lock()
		p := pk.pieces[available[i]]
		if block, ok := pk.findAvailableBlock(p, peer); ok {
			reqs = append(reqs, pk.createRequest(peer, p, block))
		}
		pk.mu.Unlock()
	}

	return reqs
}

func (pk *Picker) selectRarestFirst(
	peer netip.AddrPort,
	peerBF bitfield.Bitfield,
	n int,
) []*Request {
	rarestAvail, ok := pk.availability.FirstNonEmpty()
	if !ok {
		return nil
	}

	reqs := make([]*Request, 0, n)

	for a := rarestAvail; a <= pk.availability.maxAvail && len(reqs) < n; a++ {
		bucket := pk.availability.Bucket(a)
		if len(bucket) == 0 {
			continue
		}

		for _, piece := range bucket {
			if len(reqs) >= n {
				break
			}

			pk.mu.RLock()
			if !pk.isValidPiece(piece) || !peerBF.Has(piece) {
				pk.mu.RUnlock()
				continue
			}
			pk.mu.RUnlock()

			pk.mu.Lock()
			p := pk.pieces[piece]
			if block, ok := pk.findAvailableBlock(p, peer); ok {
				reqs = append(reqs, pk.createRequest(peer, p, block))
			}
			pk.mu.Unlock()
		}
	}

	return reqs
}
