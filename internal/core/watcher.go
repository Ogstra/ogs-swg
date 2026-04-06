package core

import (
	"bufio"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var inboundConnectionPattern = regexp.MustCompile(`inbound connection from ([^\s]+)`)

type ActiveConnection struct {
	User       string
	SourceIP   string
	SourcePort string
	SeenAt     int64
}

type Watcher struct {
	logPath           string
	activeUsers       map[string]int64
	activeConnections map[string]ActiveConnection
	realIPResolver    *ClientIPCorrelation
	now               func() time.Time
	mu                sync.RWMutex
	stopChan          chan struct{}
}

func NewWatcher(logPath string, resolver ...*ClientIPCorrelation) *Watcher {
	var realIPResolver *ClientIPCorrelation
	if len(resolver) > 0 {
		realIPResolver = resolver[0]
	}

	return &Watcher{
		logPath:           logPath,
		activeUsers:       make(map[string]int64),
		activeConnections: make(map[string]ActiveConnection),
		realIPResolver:    realIPResolver,
		now:               time.Now,
		stopChan:          make(chan struct{}),
	}
}

func (w *Watcher) Start() {
	go w.pollLoop()
}

func (w *Watcher) Stop() {
	close(w.stopChan)
}

func (w *Watcher) pollLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastSize int64 = 0

	if info, err := os.Stat(w.logPath); err == nil {
		lastSize = info.Size()
	}

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			info, err := os.Stat(w.logPath)
			if err != nil {
				continue
			}

			if info.Size() < lastSize {
				lastSize = 0
			}

			if info.Size() > lastSize {
				w.processNewLines(lastSize, info.Size())
				lastSize = info.Size()
			}
		}
	}
}

func (w *Watcher) processNewLines(start, end int64) {
	f, err := os.Open(w.logPath)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := f.Seek(start, 0); err != nil {
		return
	}

	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		w.processLine(scanner.Text())
	}
}

func (w *Watcher) GetActiveUsers(windowSeconds int64) []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var active []string
	now := w.now().Unix()
	threshold := now - windowSeconds

	for user, lastSeen := range w.activeUsers {
		if lastSeen >= threshold {
			active = append(active, user)
		}
	}
	return active
}

func (w *Watcher) GetActiveConnections(windowSeconds int64) []ActiveConnection {
	w.mu.RLock()
	defer w.mu.RUnlock()

	now := w.now().Unix()
	threshold := now - windowSeconds
	active := make([]ActiveConnection, 0, len(w.activeConnections))

	for _, conn := range w.activeConnections {
		if conn.SeenAt >= threshold {
			active = append(active, conn)
		}
	}

	return active
}

func (w *Watcher) processLine(line string) {
	user := extractEmail(line)
	if user == "" {
		return
	}

	nowUnix := w.now().Unix()
	w.mu.Lock()
	w.activeUsers[user] = nowUnix
	w.mu.Unlock()

	sourceAddr := extractInboundConnectionSource(line)
	if sourceAddr == "" {
		return
	}

	sourceIP, sourcePort := normalizeConnectionSource(sourceAddr, w.realIPResolver)
	if sourceIP == "" {
		return
	}

	w.mu.Lock()
	w.activeConnections[user] = ActiveConnection{
		User:       user,
		SourceIP:   sourceIP,
		SourcePort: sourcePort,
		SeenAt:     nowUnix,
	}
	w.mu.Unlock()
}

func extractEmail(line string) string {
	parts := strings.SplitN(line, "email:", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func extractInboundConnectionSource(line string) string {
	matches := inboundConnectionPattern.FindStringSubmatch(line)
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func normalizeConnectionSource(sourceAddr string, resolver *ClientIPCorrelation) (string, string) {
	trimmed := strings.TrimSpace(sourceAddr)
	host, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return "", ""
	}

	resolved := trimmed
	if resolver != nil {
		resolved = resolver.ResolveLoopbackRemote(trimmed)
	}

	if resolvedHost, resolvedPort, err := net.SplitHostPort(resolved); err == nil {
		if resolvedPort != "" {
			port = resolvedPort
		}
		return resolvedHost, port
	}

	if ip := strings.TrimSpace(resolved); ip != "" {
		return ip, port
	}

	return host, port
}
