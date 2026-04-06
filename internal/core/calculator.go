package core

import (
	"fmt"
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
	now          func() time.Time
	getTraffic   func([]string) (int64, int64, error)
}

func NewCalculator(w *Watcher, sb *SingboxClient, s *Store, inboundTags []string) *Calculator {
	calc := &Calculator{
		watcher:     w,
		sbClient:    sb,
		store:       s,
		inboundTags: inboundTags,
		now:         time.Now,
	}
	if sb != nil {
		calc.getTraffic = sb.GetTrafficMulti
	}
	return calc
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
	if c.getTraffic == nil {
		log.Printf("Error getting sing-box stats: %v", fmt.Errorf("traffic getter not configured"))
		return
	}

	up, down, err := c.getTraffic(c.inboundTags)
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

	activeConnections := c.watcher.GetActiveConnections(60)
	if len(activeConnections) == 0 {
		activeUsers := c.watcher.GetActiveUsers(60)
		if len(activeUsers) == 0 {
			log.Printf("Traffic detected but no active users found in logs. Dropping %d/%d bytes.", deltaUp, deltaDown)
			return
		}
		activeConnections = make([]ActiveConnection, 0, len(activeUsers))
		for _, user := range activeUsers {
			activeConnections = append(activeConnections, ActiveConnection{User: user})
		}
	}

	activeUsers := uniqueConnectionUsers(activeConnections)
	if len(activeUsers) == 0 {
		log.Printf("Traffic detected but no active users found in logs. Dropping %d/%d bytes.", deltaUp, deltaDown)
		return
	}

	count := int64(len(activeUsers))
	shareUp := deltaUp / count
	shareDown := deltaDown / count

	now := c.now().Unix()

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

func uniqueConnectionUsers(connections []ActiveConnection) []string {
	users := make([]string, 0, len(connections))
	seen := make(map[string]struct{}, len(connections))
	for _, conn := range connections {
		if conn.User == "" {
			continue
		}
		if _, ok := seen[conn.User]; ok {
			continue
		}
		seen[conn.User] = struct{}{}
		users = append(users, conn.User)
	}
	return users
}
