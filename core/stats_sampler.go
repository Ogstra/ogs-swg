package core

import (
	"log"
	"time"
	"sync"
)

type StatsSampler struct {
	sb       *SingboxClient
	store    *Store
	cfg      *Config
	last     map[string]UserCounter
	interval time.Duration
	stopCh   chan struct{}
	mu       sync.Mutex
	paused   bool
}

func NewStatsSampler(sb *SingboxClient, store *Store, cfg *Config) *StatsSampler {
	interval := time.Duration(cfg.SamplerIntervalSec) * time.Second
	if interval <= 0 {
		interval = 120 * time.Second
	}
	return &StatsSampler{
		sb:       sb,
		store:    store,
		cfg:      cfg,
		last:     make(map[string]UserCounter),
		interval: interval,
		stopCh:   make(chan struct{}),
		paused:   false,
	}
}

func (s *StatsSampler) Start() {
	go s.loop()
}

func (s *StatsSampler) Stop() {
	close(s.stopCh)
}

func (s *StatsSampler) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.sampleOnce()
		case <-s.stopCh:
			return
		}
	}
}

func (s *StatsSampler) TriggerOnce() {
	s.sampleOnce()
}

func (s *StatsSampler) SetPaused(p bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = p
}

func (s *StatsSampler) IsPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

func (s *StatsSampler) sampleOnce() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.paused {
		return
	}

	start := time.Now()
	now := time.Now().Unix()
	var batch []Sample

	users, err := LoadUsersFromSingboxConfig(s.cfg.SingboxConfigPath, s.cfg.ManagedInbounds)
	if err != nil {
		log.Printf("StatsSampler: cannot load users: %v", err)
		return
	}
	if len(users) == 0 {
		log.Printf("StatsSampler: no users found in sing-box config")
		return
	}

	stats, err := s.sb.QueryUserStats()
	if err != nil {
		log.Printf("StatsSampler: QueryUserStats error: %v", err)
		return
	}

	for _, u := range users {
		cur := stats[u.Name]
		prev, ok := s.last[u.Name]
		if ok {
			du := cur.Uplink - prev.Uplink
			dd := cur.Downlink - prev.Downlink
			if du < 0 {
				du = 0
			}
			if dd < 0 {
				dd = 0
			}
			if du > 0 || dd > 0 {
				batch = append(batch, Sample{
					User:      u.Name,
					Timestamp: now,
					Uplink:    du,
					Downlink:  dd,
				})
			}
		}
		s.last[u.Name] = UserCounter{Uplink: cur.Uplink, Downlink: cur.Downlink}
	}

	if len(batch) == 0 {
		log.Printf("StatsSampler: no deltas to insert (users=%d)", len(users))
		if s.store != nil {
			s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), 0, "")
		}
		return
	}
	if err := s.store.BulkInsert(batch); err != nil {
		log.Printf("StatsSampler: bulk insert error: %v", err)
		if s.store != nil {
			s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), int64(len(batch)), err.Error())
		}
		return
	}
	if s.store != nil {
		s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), int64(len(batch)), "")
	}
	log.Printf("StatsSampler: inserted %d samples", len(batch))
}
