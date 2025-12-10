package core

import (
	"log"
	"time"
)

type Calculator struct {
	watcher     *Watcher
	sbClient    *SingboxClient
	store       *Store
	inboundTags []string

	lastUplink   int64
	lastDownlink int64
	initialized  bool
}

func NewCalculator(w *Watcher, sb *SingboxClient, s *Store, inboundTags []string) *Calculator {
	return &Calculator{
		watcher:     w,
		sbClient:    sb,
		store:       s,
		inboundTags: inboundTags,
	}
}

func (c *Calculator) Start() {
	go c.loop()
}

func (c *Calculator) loop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.process()
	}
}

func (c *Calculator) process() {
	up, down, err := c.sbClient.GetTrafficMulti(c.inboundTags)
	if err != nil {
		log.Printf("Error getting sing-box stats: %v", err)
		return
	}

	if !c.initialized {
		c.lastUplink = up
		c.lastDownlink = down
		c.initialized = true
		return
	}

	deltaUp := up - c.lastUplink
	deltaDown := down - c.lastDownlink

	if deltaUp < 0 {
		deltaUp = up
	}
	if deltaDown < 0 {
		deltaDown = down
	}

	c.lastUplink = up
	c.lastDownlink = down

	if deltaUp == 0 && deltaDown == 0 {
		return
	}

	Stats.AddPoint(deltaUp, deltaDown)

	activeUsers := c.watcher.GetActiveUsers(60)
	if len(activeUsers) == 0 {
		log.Printf("Traffic detected but no active users found in logs. Dropping %d/%d bytes.", deltaUp, deltaDown)
		return
	}

	count := int64(len(activeUsers))
	shareUp := deltaUp / count
	shareDown := deltaDown / count

	now := time.Now().Unix()

	for _, user := range activeUsers {
		s := Sample{
			User:      user,
			Timestamp: now,
			Uplink:    shareUp,
			Downlink:  shareDown,
		}
		if err := c.store.AddSample(s); err != nil {
			log.Printf("Error saving sample for %s: %v", user, err)
		}
	}

	log.Printf("Distributed %d up / %d down among %d users", deltaUp, deltaDown, count)
}
