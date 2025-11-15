package dht

import (
	"crypto/rand"
	"crypto/sha1"
	"net"
	"sync"
	"time"
)

type TokenManager struct {
	currentSecret  [20]byte
	previousSecret [20]byte
	rotationTime   time.Time
	mu             sync.RWMutex
}

func NewTokenManager() *TokenManager {
	tm := &TokenManager{
		rotationTime: time.Now(),
	}

	rand.Read(tm.currentSecret[:])
	rand.Read(tm.previousSecret[:])

	go tm.rotateLoop()

	return tm
}

func (tm *TokenManager) Generate(ip net.IP) string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.generateWithSecret(ip, tm.currentSecret)
}

func (tm *TokenManager) Validate(ip net.IP, token string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if token == tm.generateWithSecret(ip, tm.currentSecret) {
		return true
	}

	if token == tm.generateWithSecret(ip, tm.previousSecret) {
		return true
	}

	return false
}

func (tm *TokenManager) generateWithSecret(ip net.IP, secret [20]byte) string {
	h := sha1.New()
	h.Write(ip.To4())
	h.Write(secret[:])
	return string(h.Sum(nil))
}

func (tm *TokenManager) rotateLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		tm.rotate()
	}
}

func (tm *TokenManager) rotate() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.previousSecret = tm.currentSecret
	rand.Read(tm.currentSecret[:])
	tm.rotationTime = time.Now()
}
