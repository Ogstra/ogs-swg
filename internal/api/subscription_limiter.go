package api

import (
	"sync"
	"time"
)

type subscriptionLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
}

func newSubscriptionLimiter() *subscriptionLimiter {
	return &subscriptionLimiter{requests: make(map[string][]time.Time)}
}

func (l *subscriptionLimiter) record(token string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests[token] = append(l.requests[token], time.Now())
}

func (l *subscriptionLimiter) check(token string, maxRequests int, window time.Duration) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)
	times := l.requests[token]
	start := 0
	for start < len(times) && times[start].Before(cutoff) {
		start++
	}
	times = times[start:]
	if len(times) == 0 {
		delete(l.requests, token)
		return false, 0
	}
	l.requests[token] = times

	if len(times) >= maxRequests {
		retryAfter := times[0].Add(window).Sub(now)
		return true, retryAfter
	}
	return false, 0
}
