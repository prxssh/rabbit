package config

import (
	"context"
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
	// DefaultDownloadDir is the default directory where NEW torrent files
	// are saved. Changing this only affects new torrents; existing torrents
	// continue downloading to their original location.
	DefaultDownloadDir string

	// Port is the TCP port this client listens on for incoming peer
	// connections.
	Port uint16

	// NumWant is the maximum number of peers to request the tracker.
	NumWant uint32

	// MaxUploadRate limits upload speed in bytes/second. 0 = unlimited.
	MaxUploadRate int64

	// MaxDownloadRate limits download speed in bytes/second. 0 = unlimited.
	MaxDownloadRate int64

	// AnnounceInterval overrides tracker's suggested interval.
	// 0 uses tracker default.
	AnnounceInterval time.Duration

	// MinAnnounceInterval enforces a minimum time between announces.
	MinAnnounceInterval time.Duration

	// MaxAnnounceBackoff caps exponential backoff for failed announces.
	MaxAnnounceBackoff time.Duration

	// EnableIPv6 allows connections to IPv6 peers.
	EnableIPv6 bool

	// EnableDHT enables DHT for peer discovery (future).
	EnableDHT bool

	// EnablePEX enables peer exchange protocol (future).
	EnablePEX bool

	// ClientIDPrefix customizes the peer ID prefix (e.g., "-EC0001-").
	// Must be exactly 8 bytes. Empty uses default.
	ClientIDPrefix string

	// hasIPV6 keeps track of whether or not the system supports IPV6
	// addresses.
	HasIPV6 bool

	// PieceDownloadStrategy chooses how to rank eligible pieces (see
	// Strategy).
	PieceDownloadStrategy PieceDownloadStrategy

	// MaxInflightRequests is the per-peer cap the picker should respect
	// when handing out requests to a single connection. The picker
	// doesn’t enforce per-peer counters by itself — your peer loop
	// should pass a view (capacity) and the picker should not exceed it.
	MaxInflightRequests int

	// RequestTimeout is the baseline time after which an in-flight block
	// can be considered timed-out and re-assigned. You can adapt it
	// per-peer using RTT.
	RequestTimeout time.Duration

	// EndgameDupPerBlock, when Endgame is enabled, caps the number of
	// duplicate owners (peers concurrently fetching the same block).
	EndgameDupPerBlock int

	// MaxRequestsPerBlocks limit how many duplicate blocks can be requested
	// from a single piece at once, preventing over-downloading of
	// individual blocks.
	MaxRequestsPerBlocks int

	// MaxPeers is the maximum number of concurrent peer connections
	// allowed.
	MaxPeers int

	// MaxInflightRequestsPerPeer limits how many requests can be
	// outstanding to a single peer at once.
	MaxInflightRequestsPerPeer int

	// MaxRequestsPerPiece caps the number of duplicate requests for the
	// same piece across all peers to prevent over-downloading.
	MaxRequestsPerPiece int

	// PeerHeartbeatInterval is how often to send keep-alive messages to
	// peer to maintain the connection.
	PeerHeartbeatInterval time.Duration

	// ReadTimeout is the maximum time to wait for data from a peer before
	// considering the connection stalled.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum time to wait when sending data to a peer
	// before considering the connection stalled.
	WriteTimeout time.Duration

	// DialTimeout is the maximum time to wait when establishing a new
	// connection to a peer.
	DialTimeout time.Duration

	// KeepAliveInterval is how often to check peer connection health and
	// close idle connections.
	KeepAliveInterval time.Duration

	// PeerOutboundQueueBacklog is the maximum messages that peer can have
	// in its buffer.
	PeerOutboundQueueBacklog int
}

// DefaultConfig returns sensible defaults for most use cases.
func defaultConfig() Config {
	downloadDir := getDefaultDownloadDir()

	return Config{
		DefaultDownloadDir:         downloadDir,
		Port:                       6969,
		NumWant:                    50,
		MaxUploadRate:              0, // unlimited
		MaxDownloadRate:            0, // unlimited
		AnnounceInterval:           0, // use tracker default
		MinAnnounceInterval:        2 * time.Minute,
		MaxAnnounceBackoff:         5 * time.Minute,
		EnableIPv6:                 true,
		EnableDHT:                  false,
		EnablePEX:                  false,
		ClientIDPrefix:             "-RBBT001-",
		HasIPV6:                    hasIPV6(),
		MaxInflightRequests:        10,
		RequestTimeout:             30 * time.Second,
		EndgameDupPerBlock:         2,
		MaxRequestsPerBlocks:       4,
		PieceDownloadStrategy:      PieceDownloadStrategyRarestFirst,
		MaxPeers:                   50,
		MaxInflightRequestsPerPeer: 5,
		MaxRequestsPerPiece:        4,
		PeerHeartbeatInterval:      2 * time.Minute,
		ReadTimeout:                45 * time.Second,
		WriteTimeout:               45 * time.Second,
		DialTimeout:                30 * time.Second,
		KeepAliveInterval:          2 * time.Minute,
		PeerOutboundQueueBacklog:   25,
	}
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
		return filepath.Join(
			home,
			".local",
			"share",
			"rabbit",
			"downloads",
		)
	}
}
