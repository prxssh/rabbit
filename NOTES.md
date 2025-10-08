# Piece Downloading

1. **Piece Picker**
    - Maintains availability counts and per-piece/block state.
    - Choose *which blocks* to fetch next (rarest-first, sequential, deadlines
      for streaming, endgame rules).
    - Knows partial pieces and tries to complete them.
2. **Dispatcher**
    - Decides *how many* blocks to request from *each peer* right now.
    - Uses **per-peer window** sizing logic: target window = f(rate, RTT, request_queue_time),
      clamped by a hard per-peer cap.
    - Asks the picker for exactly that many blocks for that peer and sends requests.
3. **Per-Peer State**
    - Tracks in-flight requests (set of (piece, begin, length) -> timestamp).
    - Tracks smoothed **peer throughput** (bytes/s) and **SRTT/RTO** (request->first-byte).
    - Maintains a bound **send queue** to the wire.
4. **Wire + Peer**
    - Send `REQUEST` messages; the peer replies with `PIECE` payloads.
    - Congestion, disk latency, and remote scheduling all live here.
5. **Verifier / Disk I/O**
    - Assembles blocks into piece buffers, SHA-1 verifies, writes to disk.
    - On verify OK: announces `HAVE` to others; on fail: marks blocks/pieces bad -> re-request.
6. **Choker**
    - Upload slot policy: tit-for-tat + optimistic unchoke
    - Can influence who you prioritize downloading from

## Download Loop

[1] Measure
    - peer_rate = EWMA of recent download bytes from this peer
    - SRTT/RTO = from (request -> first-byte) timestamps

[2] Compute target window (how many blocks to keep in flight)
    want = ceil(peer_rate * SRTT * RequestQueueTime / block_size)
    want = clamp(want, MinInflightRequestsPeerPeer, MaxInlightRequestsPerPeer)

[3] Top-up
    deficit = want - inflight_count(peer)
    while deficit > 0:
        blk = picker.NextForPeer(peerView)
        if none -> break
        send request(blk)
        inflight.add(blk, now)
        deficit--

[4] On piece arrival
    - if first byte: rtt_sample = now - inflight[blk].sent; SRTT/RTO update
    - write block into piece buffer; if piece complete -> hash -> on success emit `HAVE`

[5] On timeout (now - sent > RTO or RequestTimeout)
    - cancel inlflight mark; reinsert block to picker (OnTimeout)
    - endgame may keep dup request alive if other is still pending

[6] Repeat every RechokeInterval or on events (peer sped up, inflight dropped)
