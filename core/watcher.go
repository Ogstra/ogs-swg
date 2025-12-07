package core

import (
	"bufio"
	"os"
	"strings"
	"sync"
	"time"
)

type Watcher struct {
	logPath     string
	activeUsers map[string]int64
	mu          sync.RWMutex
	stopChan    chan struct{}
}

func NewWatcher(logPath string) *Watcher {
	return &Watcher{
		logPath:     logPath,
		activeUsers: make(map[string]int64),
		stopChan:    make(chan struct{}),
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
		line := scanner.Text()
		if strings.Contains(line, "email:") {
			parts := strings.Split(line, "email:")
			if len(parts) > 1 {
				user := strings.TrimSpace(parts[1])
				w.mu.Lock()
				w.activeUsers[user] = time.Now().Unix()
				w.mu.Unlock()
			}
		}
	}
}

func (w *Watcher) GetActiveUsers(windowSeconds int64) []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var active []string
	now := time.Now().Unix()
	threshold := now - windowSeconds

	for user, lastSeen := range w.activeUsers {
		if lastSeen >= threshold {
			active = append(active, user)
		}
	}
	return active
}
