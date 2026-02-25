package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type qrEntry struct {
	Config    string
	ExpiresAt time.Time
}

func (s *Server) markWireGuardPending() {
	if s.config.EnableWireGuard {
		s.wgPendingRestart = true
	}
}

func (s *Server) clearWireGuardPending() {
	s.wgPendingRestart = false
}

func (s *Server) startWireGuardSampler() {
	go func() {
		for {
			select {
			case <-s.wgSamplerTicker.C:
				s.wgMux.RLock()
				paused := s.wgSamplerPaused
				s.wgMux.RUnlock()
				if !paused {
					s.runWireGuardSample()
				}
			case <-s.wgSamplerStop:
				s.wgMux.Lock()
				if s.wgSamplerTicker != nil {
					s.wgSamplerTicker.Stop()
				}
				s.wgMux.Unlock()
				return
			}
		}
	}()
}

func (s *Server) runWireGuardSample() {
	s.wgMux.Lock()
	defer s.wgMux.Unlock()

	var stats map[string]core.PeerStats
	var err error

	if s.executor != nil {
		stats, err = s.executor.GetWireGuardStats(context.Background())
	} else {
		// Fallback (should typically have executor)
		stats, err = core.GetWireGuardStats()
	}

	if err != nil {
		log.Printf("wg sampler: failed to read stats: %v", err)
		return
	}
	handshakes := make(map[string]int64, len(stats))
	for _, st := range stats {
		if st.LatestHandshake > 0 {
			handshakes[st.PublicKey] = st.LatestHandshake
		}
	}

	var samples []core.WGSample
	now := time.Now().Unix()
	for _, st := range stats {
		prev, ok := s.wgLast[st.PublicKey]
		hasChanged := !ok || st.TransferRx != prev.Rx || st.TransferTx != prev.Tx
		if hasChanged {
			samples = append(samples, core.WGSample{
				PublicKey: st.PublicKey,
				Timestamp: now,
				Rx:        st.TransferRx,
				Tx:        st.TransferTx,
				Endpoint:  st.Endpoint,
			})
		}
		s.wgLast[st.PublicKey] = core.WGSample{
			PublicKey: st.PublicKey,
			Rx:        st.TransferRx,
			Tx:        st.TransferTx,
		}
	}

	if s.store != nil {
		start := time.Now()
		txErr := s.store.RunWGSampleTx(handshakes, samples)
		if txErr != nil {
			log.Printf("wg sampler: RunWGSampleTx error: %v", txErr)
		}
		txErrStr := ""
		if txErr != nil {
			txErrStr = txErr.Error()
		}
		s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), int64(len(samples)), txErrStr, "wireguard")
	}
}

func (s *Server) syncWireGuardConfig(wgConfig *core.WireGuardConfig) bool {
	if !s.config.EnableWireGuard {
		return false
	}

	syncPath, cleanup, err := s.writeSyncConf(wgConfig)
	if err != nil {
		log.Printf("wg syncconf prepare failed: %v", err)
		return false
	}
	defer cleanup()

	syncContent, err := os.ReadFile(syncPath)
	if err != nil {
		log.Printf("wg syncconf read generated failed: %v", err)
		return false
	}

	iface := strings.TrimSuffix(filepath.Base(s.config.WireGuardConfigPath), filepath.Ext(s.config.WireGuardConfigPath))
	if iface == "" {
		iface = "wg0"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.executor.SyncWireGuard(ctx, iface, syncContent); err != nil {
		log.Printf("wg syncconf failed: %v", err)
		return false
	}

	s.clearWireGuardPending()
	return true
}

func (s *Server) writeSyncConf(wgConfig *core.WireGuardConfig) (string, func(), error) {
	if wgConfig == nil {
		cfg, err := s.loadWireGuardConfig(context.Background())
		if err != nil {
			return "", func() {}, err
		}
		wgConfig = cfg
	}

	tmpFile, err := os.CreateTemp("", "wg-sync-*.conf")
	if err != nil {
		return "", func() {}, err
	}

	cleanup := func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}

	var b strings.Builder
	b.WriteString("[Interface]\n")
	if wgConfig.Interface.PrivateKey != "" {
		fmt.Fprintf(&b, "PrivateKey = %s\n", wgConfig.Interface.PrivateKey)
	}
	if wgConfig.Interface.ListenPort != 0 {
		fmt.Fprintf(&b, "ListenPort = %d\n", wgConfig.Interface.ListenPort)
	}
	if wgConfig.Interface.MTU != 0 {
		fmt.Fprintf(&b, "MTU = %d\n", wgConfig.Interface.MTU)
	}
	b.WriteString("\n")

	for _, p := range wgConfig.Peers {
		fmt.Fprintf(&b, "[Peer]\n")
		fmt.Fprintf(&b, "PublicKey = %s\n", p.PublicKey)
		fmt.Fprintf(&b, "AllowedIPs = %s\n", p.AllowedIPs)
		if p.Endpoint != "" {
			fmt.Fprintf(&b, "Endpoint = %s\n", p.Endpoint)
		}
		if p.PresharedKey != "" {
			fmt.Fprintf(&b, "PresharedKey = %s\n", p.PresharedKey)
		}
		fmt.Fprintf(&b, "\n")
	}

	if _, err := tmpFile.WriteString(b.String()); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return "", func() {}, err
	}

	return tmpFile.Name(), cleanup, nil
}

func (s *Server) storeQRConfig(pubKey, cfg string, ttl time.Duration) {
	if pubKey == "" || cfg == "" {
		return
	}

	s.wgQRCacheMutex.Lock()
	defer s.wgQRCacheMutex.Unlock()
	s.cleanupQRCache()
	s.wgQRCache[pubKey] = qrEntry{
		Config:    cfg,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (s *Server) fetchQRConfig(pubKey string) (string, bool) {
	s.wgQRCacheMutex.Lock()
	defer s.wgQRCacheMutex.Unlock()
	s.cleanupQRCache()
	if entry, ok := s.wgQRCache[pubKey]; ok {
		if time.Now().Before(entry.ExpiresAt) {
			return entry.Config, true
		}
		delete(s.wgQRCache, pubKey)
	}
	return "", false
}

func (s *Server) hasQRConfig(pubKey string) bool {
	_, ok := s.fetchQRConfig(pubKey)
	return ok
}

func (s *Server) cleanupQRCache() {
	now := time.Now()
	for k, v := range s.wgQRCache {
		if now.After(v.ExpiresAt) {
			delete(s.wgQRCache, k)
		}
	}
}
