package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	statsService "github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type SingboxClient struct {
	addr string
	mu   sync.Mutex
	conn *grpc.ClientConn
}

func NewSingboxClient(addr string) *SingboxClient {
	return &SingboxClient{addr: addr}
}

func (c *SingboxClient) ensureConn() (*grpc.ClientConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn, nil
	}
	conn, err := grpc.Dial(c.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	c.conn = conn
	return c.conn, nil
}

func (c *SingboxClient) GetTraffic(inboundTag string) (int64, int64, error) {
	return c.GetTrafficMulti([]string{inboundTag})
}

func (c *SingboxClient) GetTrafficMulti(tags []string) (int64, int64, error) {
	conn, err := c.ensureConn()
	if err != nil {
		return 0, 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var totalUp, totalDown int64
	for _, inboundTag := range tags {
		inboundTag = strings.TrimSpace(inboundTag)
		if inboundTag == "" {
			continue
		}
		upName := fmt.Sprintf("inbound>>>%s>>>traffic>>>uplink", inboundTag)
		var upResp statsService.GetStatsResponse
		if err := conn.Invoke(ctx, "/v2ray.core.app.stats.command.StatsService/GetStats", &statsService.GetStatsRequest{Name: upName}, &upResp); err == nil && upResp.Stat != nil {
			totalUp += upResp.Stat.Value
		}

		downName := fmt.Sprintf("inbound>>>%s>>>traffic>>>downlink", inboundTag)
		var downResp statsService.GetStatsResponse
		if err := conn.Invoke(ctx, "/v2ray.core.app.stats.command.StatsService/GetStats", &statsService.GetStatsRequest{Name: downName}, &downResp); err == nil && downResp.Stat != nil {
			totalDown += downResp.Stat.Value
		}
	}

	if totalUp == 0 && totalDown == 0 && len(tags) > 0 {
		return 0, 0, fmt.Errorf("no inbound stats found for %+v", tags)
	}

	return totalUp, totalDown, nil
}

func (c *SingboxClient) GetUserTraffic(name string) (int64, int64, error) {
	conn, err := c.ensureConn()
	if err != nil {
		return 0, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	base := fmt.Sprintf("user>>>%s>>>traffic>>>", name)
	var upResp statsService.GetStatsResponse
	if err := conn.Invoke(ctx, "/v2ray.core.app.stats.command.StatsService/GetStats", &statsService.GetStatsRequest{Name: base + "uplink"}, &upResp); err != nil {
		return 0, 0, err
	}
	var upVal int64
	if upResp.Stat != nil {
		upVal = upResp.Stat.Value
	}
	var downResp statsService.GetStatsResponse
	if err := conn.Invoke(ctx, "/v2ray.core.app.stats.command.StatsService/GetStats", &statsService.GetStatsRequest{Name: base + "downlink"}, &downResp); err != nil {
		return upVal, 0, err
	}
	var downVal int64
	if downResp.Stat != nil {
		downVal = downResp.Stat.Value
	}
	return upVal, downVal, nil
}

func (c *SingboxClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

type UserCounter struct {
	Uplink   int64
	Downlink int64
}

func (c *SingboxClient) QueryUserStats() (map[string]UserCounter, error) {
	conn, err := c.ensureConn()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var resp statsService.QueryStatsResponse
	if err := conn.Invoke(ctx, "/v2ray.core.app.stats.command.StatsService/QueryStats", &statsService.QueryStatsRequest{
		Pattern: "user>>>*>>>traffic>>>*",
		Reset_:  false,
	}, &resp); err != nil {
		return nil, err
	}

	result := make(map[string]UserCounter)
	for _, st := range resp.Stat {
		parts := strings.Split(st.Name, ">>>")
		if len(parts) != 4 {
			continue
		}
		email := parts[1]
		field := parts[3]
		cur := result[email]
		if field == "uplink" {
			cur.Uplink = st.Value
		} else if field == "downlink" {
			cur.Downlink = st.Value
		}
		result[email] = cur
	}
	return result, nil
}

type SysStats struct {
	NumGoroutine uint32 `json:"num_goroutine"`
	NumGC        uint32 `json:"num_gc"`
	Alloc        uint64 `json:"alloc"`
	TotalAlloc   uint64 `json:"total_alloc"`
	Sys          uint64 `json:"sys"`
	Mallocs      uint64 `json:"mallocs"`
	Frees        uint64 `json:"frees"`
	LiveObjects  uint64 `json:"live_objects"`
	PauseTotalNs uint64 `json:"pause_total_ns"`
	Uptime       uint32 `json:"uptime"`
}

func (c *SingboxClient) GetSysStats() (*SysStats, error) {
	conn, err := c.ensureConn()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var resp statsService.SysStatsResponse
	if err := conn.Invoke(ctx, "/v2ray.core.app.stats.command.StatsService/GetSysStats", &statsService.SysStatsRequest{}, &resp); err != nil {
		return nil, err
	}
	return &SysStats{
		NumGoroutine: resp.NumGoroutine,
		NumGC:        resp.NumGC,
		Alloc:        resp.Alloc,
		TotalAlloc:   resp.TotalAlloc,
		Sys:          resp.Sys,
		Mallocs:      resp.Mallocs,
		Frees:        resp.Frees,
		LiveObjects:  resp.LiveObjects,
		PauseTotalNs: resp.PauseTotalNs,
		Uptime:       resp.Uptime,
	}, nil
}
