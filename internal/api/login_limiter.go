package api

import (
	"sync"
	"time"
)

const (
	loginMaxFailures = 10
	loginWindow      = 15 * time.Minute
	// loginLimiterSweepSize is the map size that triggers a sweep of expired entries.
	loginLimiterSweepSize = 1000
)

type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{failures: make(map[string][]time.Time)}
}

// recordFailure records a failed login attempt for the given username.
func (l *loginLimiter) recordFailure(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	// Drop expired entries so usernames that never retry do not accumulate.
	if len(l.failures) >= loginLimiterSweepSize {
		cutoff := now.Add(-loginWindow)
		for name, times := range l.failures {
			if len(times) == 0 || times[len(times)-1].Before(cutoff) {
				delete(l.failures, name)
			}
		}
	}
	l.failures[username] = append(l.failures[username], now)
}

// isBlocked returns true and the remaining backoff duration if the username is rate-limited.
func (l *loginLimiter) isBlocked(username string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-loginWindow)

	times := l.failures[username]
	start := 0
	for start < len(times) && times[start].Before(cutoff) {
		start++
	}
	times = times[start:]
	if len(times) == 0 {
		delete(l.failures, username)
		return false, 0
	}
	l.failures[username] = times

	if len(times) >= loginMaxFailures {
		retryAfter := times[0].Add(loginWindow).Sub(now)
		return true, retryAfter
	}
	return false, 0
}
