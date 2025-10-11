package config

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// PieceDownloadStrategy enumerates high-level peice selection policies the
// picker can apply.
//
// The current code builds the state in a strategy agnostic manner; your
// selection method can switch on this value to implement different behaviours.
type PieceDownloadStrategy uint8

const (
	// PieceDownloadStrategyRandomFirst randomly samples among eligible
	// pieces (often used only for the first few pieces to reduce clumping),
	// then hands over to another strategy.
	PieceDownloadStrategyRandom PieceDownloadStrategy = iota

	// PieceDownloadStrategyRarestFirst prioritizes pieces with the lowest
	// Availability, improving swarm health and resilience.
	PieceDownloadStrategyRarestFirst

	// PieceDownloadStrategySequential downloads pieces in ascending index
	// order. Great for simplicity and streaming/locality; not ideal for
	// swarm health.
	PieceDownloadStrategySequential
)

// Config defines behavior and resource limits for a torrent download.
type Config struct {
	// ========== Identity / Paths ==========

	// DefaultDownloadDir is the default directory where NEW torrent files
	// are saved. Changing this only affects new torrents; existing torrents
	// continue downloading to their original location.
	DefaultDownloadDir string

	// ClientID is the unique identifier for our client.
	ClientID [sha1.Size]byte

	// ========== Networking ==========

	// ReadTimeout is the maximum time to wait for data from a peer before
	// considering the connection stalled.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum time to wait when sending data to a peer
	// before considering the connection stalled.
	WriteTimeout time.Duration

	// DialTimeout is the maximum time to wait when establishing a new
	// connection to a peer.
	DialTimeout time.Duration

	// MaxPeers is the maximum number of concurrent peer connections
	// allowed.
	MaxPeers int

	// ========== Tracker / Announce ==========

	// NumWant is the maximum number of peers to request the tracker.
	NumWant uint32

	// AnnounceInterval overrides tracker's suggested interval.
	// 0 uses tracker default.
	AnnounceInterval time.Duration

	// MinAnnounceInterval enforces a minimum time between announces.
	MinAnnounceInterval time.Duration

	// MaxAnnounceBackoff caps exponential backoff for failed announces.
	MaxAnnounceBackoff time.Duration

	// Port is the TCP port this client listens on for incoming peer
	// connections.
	Port uint16

	// =========== Rate Limits ==========

	// MaxUploadRate limits upload speed in bytes/second. 0 = unlimited.
	MaxUploadRate int64

	// MaxDownloadRate limits download speed in bytes/second. 0 = unlimited.
	MaxDownloadRate int64

	// RateLimitRefresh controls fill cadence; keep >=100ms to avoid jitter.
	RateLimitRefresh time.Duration

	// PeerOutboundQueueBacklog is the maximum messages that peer can have
	// in its buffer.
	PeerOutboundQueueBacklog int

	// ========== Piece Picker / Requests ==========

	// PieceDownloadStrategy chooses how to rank eligible pieces.
	PieceDownloadStrategy PieceDownloadStrategy

	// MaxInflightRequestsPerPeer limits how many requests can be
	// outstanding to a single peer at once.
	MaxInflightRequestsPerPeer int

	// MinInflightRequestsPerPeer is a soft floor so slow/latent peers still
	// make progress (1–4 is typical). The controller will never drop below
	// this.
	MinInflightRequestsPerPeer int

	// RequestQueueTime is the target amount of data (in seconds) to keep
	// pipelined per peer (libtorrent: request_queue_time). The controller
	// sizes the per-peer window ≈ ceil((peer_rate * RTT * RequestQueueTime)/block_size),
	// clamped to [MinInflightRequestsPerPeer, MaxInflightRequestsPerPeer].
	RequestQueueTime time.Duration

	// RequestTimeout is the baseline time after which an in-flight block
	// can be considered timed-out and re-assigned. You can adapt it
	// per-peer using RTT.
	RequestTimeout time.Duration

	// EndgameDupPerBlock, when Endgame is enabled, caps the number of
	// duplicate owners (peers concurrently fetching the same block).
	EndgameDupPerBlock int

	// EndgameThreshold decides when to enter endgame based on remaining blocks.
	EndgameThreshold int

	// MaxRequestsPerPiece caps the number of duplicate requests for the
	// same piece across all peers to prevent over-downloading.
	MaxRequestsPerPiece int

	// ========== Seeding / Choking ==========

	// UploadSlots is the number of regular unchoke slots.
	UploadSlots int

	// RechokeInterval is the duration of how often to reevalute choke/unchoke
	// decisions.
	RechokeInterval time.Duration

	// OptimisticUnchokeInterval is the duration of how often to rotate the
	// optimistic unchoke.
	OptimisticUnchokeInterval time.Duration

	// ========== Keepalive / Heartbeats ==========

	// PeerHeartbeatInterval is how often to send keep-alive messages to
	// peer to maintain the connection.
	PeerHeartbeatInterval time.Duration

	// PeerInactivityDuration is the minimum interval after which a peer connection
	// is considered inactive.
	PeerInactivityDuration time.Duration

	// KeepAliveInterval is the interval to send keep-alive messages to the peer.
	KeepAliveInterval time.Duration

	// ========== Miscellaneous ==========

	// MetricsEnabled toggled Prom/OTel metrics endpoint.
	MetricsEnabled bool

	// MetricsBindAddr is the the HTTP address for metrics (e.g., ":9090")
	MetricsBindAddr string

	// EnableIPv6 allows connections to IPv6 peers.
	EnableIPv6 bool

	// EnableDHT enables DHT for peer discovery (future).
	EnableDHT bool

	// EnablePEX enables peer exchange protocol (future).
	EnablePEX bool

	// HasIPV6 keeps track of whether or not the system supports IPV6
	// addresses.
	HasIPV6 bool
}

// DefaultConfig returns sensible defaults for most use cases.
func defaultConfig() (Config, error) {
	downloadDir := getDefaultDownloadDir()
	hasIPV6 := hasIPV6()

	clientID, err := generateClientID()
	if err != nil {
		return Config{}, err
	}

	return Config{
		DefaultDownloadDir:         downloadDir,
		ClientID:                   clientID,
		ReadTimeout:                30 * time.Second,
		WriteTimeout:               30 * time.Second,
		DialTimeout:                7 * time.Second,
		MaxPeers:                   50,
		NumWant:                    50,
		AnnounceInterval:           0,
		MinAnnounceInterval:        20 * time.Minute,
		MaxAnnounceBackoff:         45 * time.Minute,
		Port:                       6969,
		MaxUploadRate:              0,
		MaxDownloadRate:            0,
		RateLimitRefresh:           200 * time.Millisecond,
		PeerOutboundQueueBacklog:   256,
		PieceDownloadStrategy:      PieceDownloadStrategyRarestFirst,
		MaxInflightRequestsPerPeer: 32,
		MinInflightRequestsPerPeer: 4,
		RequestQueueTime:           3 * time.Second,
		RequestTimeout:             25 * time.Second,
		EndgameDupPerBlock:         2,
		EndgameThreshold:           30,
		MaxRequestsPerPiece:        128,
		UploadSlots:                4,
		RechokeInterval:            10 * time.Second,
		OptimisticUnchokeInterval:  30 * time.Second,
		PeerHeartbeatInterval:      60 * time.Second,
		KeepAliveInterval:          90 * time.Second,
		MetricsEnabled:             false,
		MetricsBindAddr:            ":9090",
		EnableIPv6:                 hasIPV6,
		EnableDHT:                  false,
		EnablePEX:                  false,
		HasIPV6:                    hasIPV6,
		PeerInactivityDuration:     2 * time.Minute,
	}, nil
}

func hasIPV6() bool {
	ifaces, _ := net.Interfaces()

	for _, ifi := range ifaces {
		if (ifi.Flags & net.FlagUp) == 0 {
			continue
		}
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			if ip == nil || ip.To4() != nil {
				continue
			}
			if ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() &&
				!ip.IsLoopback() {
				return true
			}
		}
	}

	return false
}

func getDefaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		if cwd, err := os.Getwd(); err == nil {
			return filepath.Join(cwd, "downloads")
		}
		return "./downloads"
	}

	switch runtime.Environment(context.Background()).Platform {
	case "windows":
		return filepath.Join(home, "Downloads", "rabbit")
	case "darwin":
		return filepath.Join(home, "Downloads", "rabbit")
	default: // linux, bsd, etc.
		return filepath.Join(home, ".local", "share", "rabbit", "downloads")
	}
}

func generateClientID() ([sha1.Size]byte, error) {
	var peerID [sha1.Size]byte

	prefix := []byte("-RBBT-")
	copy(peerID[:], prefix)

	if _, err := rand.Read(peerID[len(prefix):]); err != nil {
		return [sha1.Size]byte{}, err
	}

	return peerID, nil
}
