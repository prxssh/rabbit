package piece

import (
	"net/netip"
	"time"

	"github.com/prxssh/rabbit/pkg/config"
	"github.com/prxssh/rabbit/pkg/utils/bitfield"
)

// selectSequentialPiecesToDownload implements StrategySequential.
//
// It advances (nextPiece, nextBlock) cursors, skipping verified pieces, and
// emits up to 'limit' block requests for the next eligible piece that the peer
// 'bf' actually has. Each returned request is registered as in-flight
// (ownership recorded).
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
		return []*Request{}
	}

	ps := pk.pieces[pk.nextPiece]
	if pk.wanted != nil && !pk.wanted[ps.index] {
		return []*Request{}
	}
	if !bf.Has(ps.index) {
		return []*Request{}
	}

	requests := make([]*Request, 0, limit)
	bi := pk.nextBlock

	for len(requests) < limit && bi < ps.blockCount {
		blk := ps.blocks[bi]
		if blk.status != blockWant ||
			blk.pendingRequests >= config.Load().MaxRequestsPerBlocks {
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

// selectRarestPiecesForDownload implements StrategyRarestFirst.
//
// It iterates availability buckets from rarest to most common and, for each
// eligible piece the peer has, assigns WANT blocks up to 'limit'. Ownership and
// per-piece duplicate caps are respected.
func (pk *Picker) selectRarestPiecesForDownload(
	peer netip.AddrPort,
	bf bitfield.Bitfield,
	limit int,
) []*Request {
	requests := make([]*Request, 0, limit)

	maxAvailability := config.Load().MaxPeers
	for avail := 0; avail <= maxAvailability && len(requests) < limit; avail++ {
		bucket := pk.availability.Bucket(avail)
		if len(bucket) == 0 {
			continue
		}

		for _, pieceIdx := range bucket {
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
					blk.pendingRequests >= config.Load().MaxRequestsPerBlocks {
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

// selectRandomFirstPiecesForDownload implements StrategyRandomFirst.
//
// It shuffles the set of eligible pieces the peer has and assigns WANT blocks
// up to 'limit'. Useful to de-clump early piece selection before switching to a
// more structured strategy (e.g., rarest-first).
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
				blk.pendingRequests >= config.Load().MaxRequestsPerBlocks {
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

// assignBlockToPeer records ownership for (pieceIdx, blockIdx) by 'peer' and
// returns a concrete Request describing the block transfer.
//
// Side effects:
//   - marks the block inflight and increments pendingRequests
//   - updates owners map with send timestamp (for timeout handling)
//   - updates reverse indices (peerBlockAssignments, peerInflightCount)
func (pk *Picker) assignBlockToPeer(
	peer netip.AddrPort,
	pieceIdx, blockIdx int,
) *Request {
	piece := pk.pieces[pieceIdx]
	block := piece.blocks[blockIdx]
	begin, length, _ := BlockBounds(piece.length, blockIdx)

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
//
// Layout: high 32 bits = pieceIdx, low 32 bits = blockIdx.
func packKey(pieceIdx, blockIdx int) uint64 {
	return (uint64(uint32(pieceIdx)) << 32) | uint64(uint32(blockIdx))
}
