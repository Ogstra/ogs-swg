package core

import (
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// DetectPublicIP attempts to detect the public IP address
// It priorities:
// 1. OGS_PUBLIC_IP environment variable
// 2. External IP detection services (HTTP)
// 3. Local interface inspection (fallback)
func DetectPublicIP() string {
	// 1. Check Env Var
	if env := strings.TrimSpace(os.Getenv("OGS_PUBLIC_IP")); env != "" {
		return env
	}

	// 2. Client for external requests with short timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	services := []string{
		"https://api.ipify.org?format=text",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
		"http://checkip.amazonaws.com",
	}

	for _, url := range services {
		resp, err := client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			ip := strings.TrimSpace(string(body))
			if ip != "" && net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// 3. Fallback to local interfaces (mostly for non-container usage)
	return detectLocalIP()
}

func detectLocalIP() string {
	// Try to get IP from eth0 first
	if ip := getIPFromInterface("eth0"); ip != "" && !isPrivateIP(ip) {
		return ip
	}
	// Fallback to ens3 (common in cloud VMs)
	if ip := getIPFromInterface("ens3"); ip != "" && !isPrivateIP(ip) {
		return ip
	}
	// Fallback to any non-loopback, non-private interface
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ipNet, ok := addr.(*net.IPNet)
				if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
					continue
				}
				ip := ipNet.IP.String()
				// Skip private IPs
				if !isPrivateIP(ip) {
					return ip
				}
			}
		}
	}
	return ""
}

func getIPFromInterface(name string) string {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}
	return ""
}

func isPrivateIP(ip string) bool {
	return strings.HasPrefix(ip, "10.") ||
		strings.HasPrefix(ip, "192.168.") ||
		strings.HasPrefix(ip, "172.16.") ||
		strings.HasPrefix(ip, "172.17.") ||
		strings.HasPrefix(ip, "172.18.") ||
		strings.HasPrefix(ip, "172.19.") ||
		strings.HasPrefix(ip, "172.20.") ||
		strings.HasPrefix(ip, "172.21.") ||
		strings.HasPrefix(ip, "172.22.") ||
		strings.HasPrefix(ip, "172.23.") ||
		strings.HasPrefix(ip, "172.24.") ||
		strings.HasPrefix(ip, "172.25.") ||
		strings.HasPrefix(ip, "172.26.") ||
		strings.HasPrefix(ip, "172.27.") ||
		strings.HasPrefix(ip, "172.28.") ||
		strings.HasPrefix(ip, "172.29.") ||
		strings.HasPrefix(ip, "172.30.") ||
		strings.HasPrefix(ip, "172.31.")
}
