package core

import (
	"sync"
	"time"
)

type TrafficPoint struct {
	Timestamp int64 `json:"timestamp"`
	Uplink    int64 `json:"uplink"`
	Downlink  int64 `json:"downlink"`
}

type SystemStats struct {
	History []TrafficPoint
	mu      sync.RWMutex
}

var Stats = &SystemStats{
	History: make([]TrafficPoint, 0),
}

func (s *SystemStats) AddPoint(up, down int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	s.History = append(s.History, TrafficPoint{
		Timestamp: now,
		Uplink:    up,
		Downlink:  down,
	})

	if len(s.History) > 5000 {
		s.History = s.History[len(s.History)-5000:]
	}
}

func (s *SystemStats) GetHistory(duration time.Duration) []TrafficPoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if duration == 0 {
		return s.History
	}

	cutoff := time.Now().Add(-duration).Unix()
	var result []TrafficPoint = []TrafficPoint{}

	for _, p := range s.History {
		if p.Timestamp >= cutoff {
			result = append(result, p)
		}
	}
	return result
}


