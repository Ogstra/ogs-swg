package sys

import (
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func parseWGDumpStats(output []byte) map[string]core.PeerStats {
	stats := make(map[string]core.PeerStats)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		// Peer line format:
		// <intf> <pubkey> <psk> <endpoint> <allowed-ips> <handshake> <rx> <tx> <keepalive>
		if len(parts) < 8 {
			continue
		}
		pubKey := parts[1]
		if len(pubKey) < 40 {
			continue
		}
		endpoint := parts[3]
		if endpoint == "(none)" {
			endpoint = ""
		}
		latestHandshake, _ := strconv.ParseInt(parts[5], 10, 64)
		transferRx, _ := strconv.ParseInt(parts[6], 10, 64)
		transferTx, _ := strconv.ParseInt(parts[7], 10, 64)
		stats[pubKey] = core.PeerStats{
			PublicKey:       pubKey,
			Endpoint:        endpoint,
			LatestHandshake: latestHandshake,
			TransferRx:      transferRx,
			TransferTx:      transferTx,
		}
	}
	return stats
}

func parseWGTextStats(output []byte) map[string]core.PeerStats {
	stats := make(map[string]core.PeerStats)
	lines := strings.Split(string(output), "\n")
	var currentPeer string

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "peer: ") {
			currentPeer = strings.TrimSpace(strings.TrimPrefix(line, "peer: "))
			if currentPeer != "" {
				if _, ok := stats[currentPeer]; !ok {
					stats[currentPeer] = core.PeerStats{PublicKey: currentPeer}
				}
			}
			continue
		}
		if currentPeer == "" {
			continue
		}

		ps := stats[currentPeer]
		switch {
		case strings.HasPrefix(line, "endpoint: "):
			v := strings.TrimSpace(strings.TrimPrefix(line, "endpoint: "))
			if v == "(none)" {
				v = ""
			}
			ps.Endpoint = v
		case strings.HasPrefix(line, "latest handshake: "):
			v := strings.TrimSpace(strings.TrimPrefix(line, "latest handshake: "))
			ps.LatestHandshake = parseWGRelativeHandshake(v)
		case strings.HasPrefix(line, "transfer: "):
			v := strings.TrimSpace(strings.TrimPrefix(line, "transfer: "))
			parts := strings.Split(v, ",")
			if len(parts) >= 2 {
				rxText := strings.TrimSpace(strings.TrimSuffix(parts[0], "received"))
				txText := strings.TrimSpace(strings.TrimSuffix(parts[1], "sent"))
				ps.TransferRx = parseWGHumanBytes(rxText)
				ps.TransferTx = parseWGHumanBytes(txText)
			}
		}
		stats[currentPeer] = ps
	}
	return stats
}

func parseWGHumanBytes(v string) int64 {
	fields := strings.Fields(strings.TrimSpace(v))
	if len(fields) < 2 {
		return 0
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || n < 0 {
		return 0
	}
	unit := strings.ToUpper(strings.TrimSpace(fields[1]))
	mul := float64(1)
	switch unit {
	case "B", "BYTE", "BYTES":
		mul = 1
	case "KIB":
		mul = 1024
	case "MIB":
		mul = 1024 * 1024
	case "GIB":
		mul = 1024 * 1024 * 1024
	case "TIB":
		mul = 1024 * 1024 * 1024 * 1024
	}
	return int64(n * mul)
}

func parseWGRelativeHandshake(v string) int64 {
	v = strings.TrimSpace(strings.TrimSuffix(v, "ago"))
	if v == "" || strings.EqualFold(v, "(none)") || strings.EqualFold(v, "never") {
		return 0
	}
	total := int64(0)
	for _, token := range strings.Split(v, ",") {
		token = strings.TrimSpace(token)
		parts := strings.Fields(token)
		if len(parts) < 2 {
			continue
		}
		n, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || n < 0 {
			continue
		}
		unit := strings.ToLower(parts[1])
		switch {
		case strings.HasPrefix(unit, "sec"):
			total += n
		case strings.HasPrefix(unit, "min"):
			total += n * 60
		case strings.HasPrefix(unit, "hour"):
			total += n * 3600
		case strings.HasPrefix(unit, "day"):
			total += n * 86400
		case strings.HasPrefix(unit, "week"):
			total += n * 7 * 86400
		}
	}
	if total <= 0 {
		return 0
	}
	return time.Now().Unix() - total
}
