package core

import (
	"bufio"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	clientIPPortPattern    = regexp.MustCompile(`(?i)^\s*(?:client=|remote_addr=)?((?:\d{1,3}\.){3}\d{1,3}|[0-9a-f:]+):(\d{1,5})(?:$|[\s,])`)
	clientIPFieldPattern   = regexp.MustCompile(`(?i)(?:^|[\s,])(?:client|remote_addr|real_ip|realip_remote_addr)=([0-9a-f\.:]+)(?:$|[\s,])`)
	remotePortFieldPattern = regexp.MustCompile(`(?i)(?:^|[\s,])(?:remote_port|src_port|client_port)=(\d{1,5})(?:$|[\s,])`)
)

type clientIPCorrelationEntry struct {
	ip         string
	observedAt time.Time
}

type ClientIPCorrelation struct {
	ttl             time.Duration
	cleanupInterval time.Duration
	resolverMode    string
	logPath         string

	mu          sync.RWMutex
	entries     map[string]clientIPCorrelationEntry
	now         func() time.Time
	stopChan    chan struct{}
	stopOnce    sync.Once
	started     bool
	initialSize int64
}

func NewClientIPCorrelation(ttlSeconds, cleanupIntervalSeconds int, resolverMode, logPath string) *ClientIPCorrelation {
	if ttlSeconds <= 0 {
		ttlSeconds = defaultRealIPCacheTTLSec
	}
	if cleanupIntervalSeconds <= 0 {
		cleanupIntervalSeconds = defaultRealIPCleanupIntervalSec
	}

	return &ClientIPCorrelation{
		ttl:             time.Duration(ttlSeconds) * time.Second,
		cleanupInterval: time.Duration(cleanupIntervalSeconds) * time.Second,
		resolverMode:    strings.TrimSpace(strings.ToLower(resolverMode)),
		logPath:         strings.TrimSpace(logPath),
		entries:         make(map[string]clientIPCorrelationEntry),
		now:             time.Now,
		stopChan:        make(chan struct{}),
	}
}

func (c *ClientIPCorrelation) Start() {
	c.mu.Lock()
	if c.started || c.logPath == "" {
		c.mu.Unlock()
		return
	}
	if info, err := os.Stat(c.logPath); err == nil {
		c.initialSize = info.Size()
	}
	c.started = true
	c.mu.Unlock()

	go c.pollLoop()
	go c.cleanupLoop()
}

func (c *ClientIPCorrelation) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopChan)
	})
}

func (c *ClientIPCorrelation) ObserveNginxStreamLine(line string) bool {
	ip, port, ok := parseNginxStreamCorrelation(line)
	if !ok {
		return false
	}

	c.mu.Lock()
	c.entries[port] = clientIPCorrelationEntry{
		ip:         ip,
		observedAt: c.now(),
	}
	c.mu.Unlock()
	return true
}

func (c *ClientIPCorrelation) ResolveLoopbackRemote(remoteAddr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	if !isLoopbackHost(host) || port == "" || c.resolverMode != defaultRealIPResolverMode {
		return strings.TrimSpace(remoteAddr)
	}

	now := c.now()

	c.mu.RLock()
	entry, ok := c.entries[port]
	c.mu.RUnlock()
	if !ok || now.Sub(entry.observedAt) > c.ttl {
		return strings.TrimSpace(remoteAddr)
	}
	return entry.ip
}

func (c *ClientIPCorrelation) CleanupExpired() {
	now := c.now()

	c.mu.Lock()
	for port, entry := range c.entries {
		if now.Sub(entry.observedAt) > c.ttl {
			delete(c.entries, port)
		}
	}
	c.mu.Unlock()
}

func (c *ClientIPCorrelation) cleanupLoop() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.CleanupExpired()
		}
	}
}

func (c *ClientIPCorrelation) pollLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	c.mu.RLock()
	lastSize := c.initialSize
	c.mu.RUnlock()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			info, err := os.Stat(c.logPath)
			if err != nil {
				continue
			}
			if info.Size() < lastSize {
				lastSize = 0
			}
			if info.Size() > lastSize {
				c.processNewLines(lastSize)
				lastSize = info.Size()
			}
		}
	}
}

func (c *ClientIPCorrelation) processNewLines(start int64) {
	f, err := os.Open(c.logPath)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := f.Seek(start, 0); err != nil {
		return
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		c.ObserveNginxStreamLine(scanner.Text())
	}
}

func parseNginxStreamCorrelation(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", "", false
	}

	if matches := clientIPFieldPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
		ip := normalizeObservedIP(matches[1])
		if ip == "" {
			return "", "", false
		}
		if portMatches := remotePortFieldPattern.FindStringSubmatch(trimmed); len(portMatches) == 2 {
			port := normalizeObservedPort(portMatches[1])
			if port != "" {
				return ip, port, true
			}
		}
	}

	if matches := clientIPPortPattern.FindStringSubmatch(trimmed); len(matches) == 3 {
		ip := normalizeObservedIP(matches[1])
		port := normalizeObservedPort(matches[2])
		if ip != "" && port != "" {
			return ip, port, true
		}
	}

	return "", "", false
}

func normalizeObservedIP(raw string) string {
	ip := net.ParseIP(strings.Trim(raw, "[]"))
	if ip == nil {
		return ""
	}
	return ip.String()
}

func normalizeObservedPort(raw string) string {
	port := strings.TrimSpace(raw)
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 || value > 65535 {
		return ""
	}
	return strconv.Itoa(value)
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
