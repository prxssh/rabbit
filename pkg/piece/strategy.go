package piece

import (
	"net/netip"
	"time"

	"github.com/prxssh/rabbit/pkg/utils/bitfield"
)

// Strategy enumerates high-level peice selection policies the picker can apply.
//
// The current code builds the state in a strategy agnostic manner; your
// selection method can switch on this value to implement different behaviours.
type Strategy uint8

const (
	// StrategyRarestFirst prioritizes pieces with the lowest Availability,
	// improving swarm health and resilience.
	StrategyRarestFirst Strategy = iota

	// StrategySequential downloads pieces in ascending index order. Great
	// for simplicity and streaming/locality; not ideal for swarm health.
	StrategySequential

	// StrategyPriority uses a per-piece Priority field (lower = more
	// important) to bias selection.
	StrategyPriority

	// StrategyRandomFirst randomly samples among eligible pieces (often
	// used only for the first few pieces to reduce clumping), then hands
	// over to another strategy.
	StrategyRandomFirst
)

func (pk *Picker) selectSequentialPiecesToDownload(
	peer netip.AddrPort,
	bf bitfield.Bitfield,
	limit int,
) []*Request {
	for pk.nextPiece < pk.PieceCount && pk.pieces[pk.nextPiece].verified {
		pk.nextPiece++
		pk.nextBlock = 0
	}
	if pk.nextPiece >= pk.PieceCount {
		return nil
	}

	ps := pk.pieces[pk.nextPiece]
	if pk.wanted != nil && !pk.wanted[ps.index] {
		return nil
	}
	if !bf.Has(ps.index) {
		return nil
	}

	requests := make([]*Request, 0, limit)
	bi := pk.nextBlock

	for len(requests) < limit && bi < ps.blockCount {
		blk := ps.blocks[bi]
		if blk.status != blockWant ||
			blk.pendingRequests >= pk.cfg.MaxRequestsPerBlocks {
			bi++
			continue
		}

		requests = append(
			requests,
			pk.assignBlockToPeer(peer, ps.index, bi),
		)
		bi++

	}

	pk.nextBlock = bi
	return requests
}

func (pk *Picker) selectRarestPiecesForDownload(
	peer netip.AddrPort,
	bf bitfield.Bitfield,
	limit int,
) []*Request {
	requests := make([]*Request, 0, limit)

	for avail := 0; avail <= 150 && len(requests) < limit; avail++ {
		bucket, exists := pk.availabilityBuckets[avail]
		if !exists || len(bucket) == 0 {
			continue
		}

		for pieceIdx := range bucket {
			if len(requests) >= limit {
				break
			}

			ps := pk.pieces[pieceIdx]
			if ps.verified || !bf.Has(pieceIdx) {
				continue
			}
			if pk.wanted != nil && !pk.wanted[pieceIdx] {
				continue
			}

			for bi := 0; bi < ps.blockCount && len(requests) < limit; bi++ {
				blk := ps.blocks[bi]
				if blk.status != blockWant ||
					blk.pendingRequests >= pk.cfg.MaxRequestsPerBlocks {
					continue
				}

				requests = append(
					requests,
					pk.assignBlockToPeer(
						peer,
						ps.index,
						bi,
					),
				)
			}
		}
	}

	return requests
}

func (pk *Picker) selectRandomFirstPiecesForDownload(
	peer netip.AddrPort,
	bf bitfield.Bitfield,
	limit int,
) []*Request {
	eligiblePieces := make([]int, 0, pk.PieceCount)

	for i := 0; i < pk.PieceCount; i++ {
		ps := pk.pieces[i]
		if ps.verified || !bf.Has(i) {
			continue
		}
		if pk.wanted != nil && !pk.wanted[i] {
			continue
		}

		eligiblePieces = append(eligiblePieces, i)
	}

	pk.rng.Shuffle(len(eligiblePieces), func(i, j int) {
		eligiblePieces[i], eligiblePieces[j] = eligiblePieces[j], eligiblePieces[i]
	})

	requests := make([]*Request, 0, limit)

	for _, pieceIdx := range eligiblePieces {
		if len(requests) >= limit {
			break
		}

		ps := pk.pieces[pieceIdx]
		for bi := 0; bi < ps.blockCount && len(requests) < limit; bi++ {
			blk := ps.blocks[bi]
			if blk.status != blockWant ||
				blk.pendingRequests >= pk.cfg.MaxRequestsPerBlocks {
				continue
			}

			requests = append(
				requests,
				pk.assignBlockToPeer(peer, ps.index, bi),
			)

		}
	}

	return requests
}

func (pk *Picker) assignBlockToPeer(
	peer netip.AddrPort,
	pieceIdx, blockIdx int,
) *Request {
	piece := pk.pieces[pieceIdx]
	block := piece.blocks[blockIdx]

	begin := blockIdx * pk.BlockLength
	length := pk.BlockLength
	if blockIdx == piece.blockCount-1 {
		length = piece.lastBlock
	}

	block.status = blockInflight
	block.pendingRequests++
	block.owners[peer] = &ownerMeta{sentAt: time.Now()}

	key := packKey(piece.index, blockIdx)
	if pk.peerBlockAssignments[peer] == nil {
		pk.peerBlockAssignments[peer] = make(
			map[uint64]struct{},
		)
	}
	pk.peerBlockAssignments[peer][key] = struct{}{}
	pk.peerInflightCount[peer]++

	return &Request{
		Peer:   peer,
		Piece:  piece.index,
		Begin:  begin,
		Length: length,
	}
}

// packKey encodes (piece, block) into a compact uint64 for reverse indexing.
func packKey(pieceIdx, blockIdx int) uint64 {
	return (uint64(uint32(pieceIdx)) << 32) | uint64(uint32(blockIdx))
}
