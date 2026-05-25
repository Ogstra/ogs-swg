package core

import (
	"bufio"
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// LogIngester tails AccessLogPath every 1 second, inserts new lines into
// LogStore, and maintains an in-memory active-users map so Calculator keeps
// working without modification to its call sites.
type LogIngester struct {
	logPath     string
	store       *LogStore
	activeUsers map[string]int64
	mu          sync.RWMutex
	stopChan    chan struct{}
}

// NewLogIngester creates a LogIngester. Call Start() to begin tailing.
func NewLogIngester(logPath string, store *LogStore) *LogIngester {
	return &LogIngester{
		logPath:     logPath,
		store:       store,
		activeUsers: make(map[string]int64),
		stopChan:    make(chan struct{}),
	}
}

// Start launches the poll loop and retention ticker in background goroutines.
func (i *LogIngester) Start() {
	go i.pollLoop()
	go i.retentionLoop()
}

// Stop signals both background goroutines to exit.
func (i *LogIngester) Stop() {
	close(i.stopChan)
}

// GetActiveUsers returns users who were seen in the log within the last
// windowSeconds seconds. Signature is identical to Watcher.GetActiveUsers so
// Calculator can accept either via the activeUserSource interface.
func (i *LogIngester) GetActiveUsers(windowSeconds int64) []string {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var active []string
	now := time.Now().Unix()
	threshold := now - windowSeconds

	for user, lastSeen := range i.activeUsers {
		if lastSeen >= threshold {
			active = append(active, user)
		}
	}
	return active
}

// pollLoop is the main 1-second tail loop.
func (i *LogIngester) pollLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Start from EOF — do not replay historical lines (D-14).
	var lastSize int64
	if info, err := os.Stat(i.logPath); err == nil {
		lastSize = info.Size()
	}

	for {
		select {
		case <-i.stopChan:
			return
		case <-ticker.C:
			info, err := os.Stat(i.logPath)
			if err != nil {
				continue
			}

			// Rotation: file shrank (truncate or replace).
			if info.Size() < lastSize {
				lastSize = 0
			}

			if info.Size() > lastSize {
				i.processNewLines(lastSize, info.Size())
				lastSize = info.Size()
			}
		}
	}
}

// retentionLoop calls CheckRetention every 5 minutes using settings stored in
// the app_settings table (loaded lazily via the Store). For now it reads the
// defaults (size mode, 200 MB) until the Settings UI in 49-04 wires user prefs.
func (i *LogIngester) retentionLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-i.stopChan:
			return
		case <-ticker.C:
			ctx := context.Background()
			coldDir := coldDirFor(i.store.path)
			seg, err := i.store.CheckRetention(ctx, "size", 200, 0, "days", coldDir)
			if err != nil {
				log.Printf("log ingester: retention check error: %v", err)
				continue
			}
			if seg != nil {
				log.Printf("log ingester: exported cold segment %s (%d rows, %.1f KB)",
					seg.Filename, seg.RowCount, float64(seg.SizeBytes)/1024)
			}
		}
	}
}

// coldDirFor derives the cold-segment directory from the log DB path.
// e.g. "data/singbox_logs.db" → "data/logs"
func coldDirFor(dbPath string) string {
	// Place cold segments next to the DB file in a "logs" subdirectory.
	dir := dbPath
	// Strip filename to get parent dir.
	for k := len(dir) - 1; k >= 0; k-- {
		if dir[k] == '/' || dir[k] == '\\' {
			dir = dir[:k]
			break
		}
	}
	return dir + "/logs"
}

// processNewLines reads bytes [start, end) from the log file, parses each line,
// updates the active-users map, and inserts all lines into LogStore in one tx.
func (i *LogIngester) processNewLines(start, end int64) {
	f, err := os.Open(i.logPath)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := f.Seek(start, 0); err != nil {
		return
	}

	// 8 MB scanner buffer — matches readAllFileLines in other log handlers.
	const maxBuf = 8 * 1024 * 1024
	buf := make([]byte, maxBuf)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(buf, maxBuf)

	var lines []string
	now := time.Now().Unix()

	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)

		// Track active users by email: marker.
		// Split on "email:" and take the first whitespace-delimited token so
		// that trailing log text (e.g. "inbound connection") is not included.
		if strings.Contains(line, "email:") {
			parts := strings.SplitN(line, "email:", 2)
			if len(parts) == 2 {
				rest := strings.TrimSpace(parts[1])
				// Take only the first token — the actual username.
				fields := strings.Fields(rest)
				if len(fields) > 0 {
					i.mu.Lock()
					i.activeUsers[fields[0]] = now
					i.mu.Unlock()
				}
			}
		}
	}

	if len(lines) == 0 {
		return
	}

	ctx := context.Background()
	if err := i.store.InsertLogs(ctx, time.Now().UnixMilli(), lines); err != nil {
		log.Printf("log ingester: insert error: %v", err)
	}
}
