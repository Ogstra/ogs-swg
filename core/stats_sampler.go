package core

import (
	"log"
	"os"
	"sync"
	"time"
)

type StatsSampler struct {
	sb                *SingboxClient
	store             *Store
	cfg               *Config
	last              map[string]UserCounter
	interval          time.Duration
	stopCh            chan struct{}
	mu                sync.Mutex
	paused            bool
	cachedUsers       []UserAccount
	lastConfigModTime time.Time
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

func (s *StatsSampler) loadUsersIfNeeded() ([]UserAccount, error) {
	info, err := os.Stat(s.cfg.SingboxConfigPath)
	if err != nil {
		return nil, err
	}
	modTime := info.ModTime()

	if modTime.Equal(s.lastConfigModTime) && s.cachedUsers != nil {
		return s.cachedUsers, nil
	}

	users, err := LoadUsersFromSingboxConfig(s.cfg.SingboxConfigPath, s.cfg.ManagedInbounds)
	if err != nil {
		return nil, err
	}

	s.cachedUsers = users
	s.lastConfigModTime = modTime
	// log.Printf("StatsSampler: reloaded users from config (modTime: %v)", modTime)
	return users, nil
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

	users, err := s.loadUsersIfNeeded()
	if err != nil {
		log.Printf("StatsSampler: cannot load users: %v", err)
		return
	}
	if len(users) == 0 {
		// log.Printf("StatsSampler: no users found in sing-box config")
		return
	}

	stats, err := s.sb.QueryUserStats()
	if err != nil {
		log.Printf("StatsSampler: QueryUserStats error: %v", err)
		return
	}

	// 1. Calculate Deltas
	activeUserNames := make(map[string]bool)
	for _, u := range users {
		activeUserNames[u.Name] = true
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

	// 2. Prune 'last' map (Fix Memory Leak)
	// Remove users that are no longer in the active user list
	for name := range s.last {
		if !activeUserNames[name] {
			delete(s.last, name)
		}
	}

	if len(batch) == 0 {
		// log.Printf("StatsSampler: no deltas to insert (users=%d)", len(users))
		if s.store != nil {
			s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), 0, "", "sing-box")
		}
		return
	}
	if err := s.store.BulkInsert(batch); err != nil {
		log.Printf("StatsSampler: bulk insert error: %v", err)
		if s.store != nil {
			s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), int64(len(batch)), err.Error(), "sing-box")
		}
		return
	}
	if s.store != nil {
		s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), int64(len(batch)), "", "sing-box")
	}
	log.Printf("StatsSampler: inserted %d samples", len(batch))
}
