package api

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func demoModeLastSeen(scope, key string, now time.Time) int64 {
	bucket := now.Unix() / 90
	seed := stableHash64(scope + "|" + key + "|" + strconv.FormatInt(bucket, 10))
	roll := seed % 100
	switch {
	case roll < 58:
		return demoModeLastSeenInRange(scope+"|hot", key, now, 5, 150)
	case roll < 83:
		return demoModeLastSeenInRange(scope+"|warm", key, now, 3*60, 35*60)
	default:
		return demoModeLastSeenInRange(scope+"|cold", key, now, 35*60, 9*60*60)
	}
}

func demoModeLastSeenInRange(scope, key string, now time.Time, minOffset, maxOffset int64) int64 {
	base := now.Unix()
	if base <= 0 {
		return 0
	}
	if minOffset < 0 {
		minOffset = 0
	}
	if maxOffset < minOffset {
		maxOffset = minOffset
	}

	bucket := now.Unix() / 90
	offsetSeed := stableHash64("offset|" + scope + "|" + key + "|" + strconv.FormatInt(bucket, 10))
	span := maxOffset - minOffset + 1
	if span < 1 {
		span = 1
	}
	offset := minOffset + int64(offsetSeed%uint64(span))
	ts := base - offset
	if ts < 1 {
		return 1
	}
	return ts
}

func stableHash64(v string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(v))
	return h.Sum64()
}

func wireGuardPeerDisplayName(publicKey string, meta core.WGPeerMeta, preferred map[string]string) string {
	if v, ok := preferred[publicKey]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	if strings.TrimSpace(meta.Alias) != "" {
		return meta.Alias
	}
	if len(publicKey) >= 8 {
		return publicKey[:8]
	}
	return publicKey
}

func demoSelectActiveKeys(scope string, keys []string, now time.Time, minCount, maxCount int) map[string]struct{} {
	active := make(map[string]struct{})
	if len(keys) == 0 {
		return active
	}

	sort.Strings(keys)
	if minCount < 1 {
		minCount = 1
	}
	if maxCount < minCount {
		maxCount = minCount
	}
	if minCount > len(keys) {
		minCount = len(keys)
	}
	if maxCount > len(keys) {
		maxCount = len(keys)
	}

	bucket := now.Unix() / 300 // 5-minute windows
	target := minCount
	span := maxCount - minCount + 1
	if span > 1 {
		target += int(stableHash64("count|"+scope+"|"+strconv.FormatInt(bucket, 10)) % uint64(span))
	}

	start := int(stableHash64("start|"+scope+"|"+strconv.FormatInt(bucket, 10)) % uint64(len(keys)))
	step := 1
	if len(keys) > 1 {
		step += int(stableHash64("step|"+scope+"|"+strconv.FormatInt(bucket, 10)) % uint64(len(keys)-1))
	}

	for k := 0; len(active) < target && k < len(keys)*2; k++ {
		idx := (start + k*step) % len(keys)
		active[keys[idx]] = struct{}{}
	}
	for len(active) < target {
		idx := len(active) % len(keys)
		active[keys[idx]] = struct{}{}
	}
	return active
}

func (s *Server) demoActiveWireGuardPeers(threshold int64, preferred map[string]string, now time.Time) []string {
	if !s.config.DemoMode || s.store == nil {
		return nil
	}
	peers, err := s.store.GetWGPeerMeta()
	if err != nil || len(peers) == 0 {
		return nil
	}

	peerKeys := make([]string, 0, len(peers))
	for publicKey := range peers {
		peerKeys = append(peerKeys, publicKey)
	}
	activeSet := demoSelectActiveKeys("wireguard-peer", peerKeys, now, 4, 5)
	out := make([]string, 0, len(activeSet))
	for publicKey, meta := range peers {
		if _, ok := activeSet[publicKey]; !ok {
			continue
		}
		lastSeen := now.Unix() - 30
		if lastSeen < threshold {
			continue
		}
		out = append(out, wireGuardPeerDisplayName(publicKey, meta, preferred))
	}
	if len(out) == 0 && len(peerKeys) > 0 {
		sort.Strings(peerKeys)
		bucket := now.Unix() / 90
		idx := int(stableHash64("wireguard-peer-fallback|"+strconv.FormatInt(bucket, 10)) % uint64(len(peerKeys)))
		key := peerKeys[idx]
		out = append(out, wireGuardPeerDisplayName(key, peers[key], preferred))
	}
	sort.Strings(out)
	return out
}
