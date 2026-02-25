package api

import (
	"net"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func singboxAPILikelySelfTarget(cfg *core.Config) bool {
	if cfg == nil || cfg.ExecutionMode != "docker_local" {
		return false
	}

	apiHost, apiPort := splitHostPortLoose(cfg.SingboxAPIAddr)
	panelHost, panelPort := splitHostPortLoose(cfg.ListenAddr)
	if apiPort == "" || panelPort == "" || apiPort != panelPort {
		return false
	}

	return isLoopbackLike(apiHost) && isLoopbackLike(panelHost)
}

func splitHostPortLoose(addr string) (string, string) {
	a := strings.TrimSpace(addr)
	if a == "" {
		return "", ""
	}
	if strings.HasPrefix(a, ":") {
		return "", strings.TrimPrefix(a, ":")
	}
	if h, p, err := net.SplitHostPort(a); err == nil {
		return h, p
	}
	return "", ""
}

func isLoopbackLike(host string) bool {
	h := strings.TrimSpace(strings.Trim(host, "[]"))
	if h == "" {
		return true
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
