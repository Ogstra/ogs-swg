package sys

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/coreos/go-systemd/v22/dbus"
	"golang.zx2c4.com/wireguard/wgctrl"
)

// AllowedSysctlKeys defines the whitelist of sysctl keys that can be modified.
var AllowedSysctlKeys = map[string]bool{
	"net.ipv4.ip_forward":                true,
	"net.ipv6.conf.all.forwarding":       true,
	"net.core.default_qdisc":             true,
	"net.ipv4.tcp_congestion_control":    true,
	"net.ipv4.conf.all.accept_redirects": true,
	"net.ipv4.conf.all.send_redirects":   true,
	"net.ipv6.conf.all.accept_redirects": true,
}

type LocalExecutor struct{}

func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

func (e *LocalExecutor) RestartService(ctx context.Context, name string) error {
	return dbusServiceAction(ctx, "restart", name)
}

func (e *LocalExecutor) StartService(ctx context.Context, name string) error {
	return dbusServiceAction(ctx, "start", name)
}

func (e *LocalExecutor) StopService(ctx context.Context, name string) error {
	return dbusServiceAction(ctx, "stop", name)
}

func (e *LocalExecutor) IsServiceActive(ctx context.Context, name string) (bool, error) {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return false, fmt.Errorf("D-Bus connect: %v", err)
	}
	defer conn.Close()

	unit := resolveUnitName(name)
	prop, err := conn.GetUnitPropertyContext(ctx, unit, "ActiveState")
	if err != nil {
		return false, nil // unit not found or not loaded
	}
	state, _ := prop.Value.Value().(string)
	return state == "active", nil
}

func (e *LocalExecutor) WriteConfig(_ context.Context, path string, content []byte, fileMode os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, fileMode); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (e *LocalExecutor) ReadConfig(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (e *LocalExecutor) ApplySysctl(_ context.Context, key, value string) error {
	if !AllowedSysctlKeys[key] {
		return fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	path := "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
	if err := os.WriteFile(path, []byte(value+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to apply sysctl %s: %v", key, err)
	}
	return nil
}

func (e *LocalExecutor) GetSysctl(_ context.Context, key string) (string, error) {
	if !AllowedSysctlKeys[key] {
		return "", fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	path := "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to get sysctl %s: %v", key, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func (e *LocalExecutor) ReadJournal(_ context.Context, unit string, limit int) ([]string, error) {
	return journalRead(unit, limit, "")
}

func (e *LocalExecutor) SearchJournal(_ context.Context, unit, query string, limit int) ([]string, error) {
	return journalRead(unit, limit, query)
}

func (e *LocalExecutor) CheckConnectivity(_ context.Context) error {
	return nil
}

func (e *LocalExecutor) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func (e *LocalExecutor) GetWireGuardStats(_ context.Context) (map[string]core.PeerStats, error) {
	stats := make(map[string]core.PeerStats)

	c, err := wgctrl.New()
	if err != nil {
		return stats, err
	}
	defer c.Close()

	devices, err := c.Devices()
	if err != nil {
		return stats, err
	}

	for _, dev := range devices {
		for _, peer := range dev.Peers {
			endpoint := ""
			if peer.Endpoint != nil {
				endpoint = peer.Endpoint.String()
			}
			handshake := peer.LastHandshakeTime.Unix()
			if peer.LastHandshakeTime.IsZero() || handshake < 0 {
				handshake = 0
			}
			stats[peer.PublicKey.String()] = core.PeerStats{
				PublicKey:       peer.PublicKey.String(),
				InterfaceName:   dev.Name,
				Endpoint:        endpoint,
				LatestHandshake: handshake,
				TransferRx:      peer.ReceiveBytes,
				TransferTx:      peer.TransmitBytes,
			}
		}
	}

	return stats, nil
}

func (e *LocalExecutor) RestartWireGuard(ctx context.Context, interfaceName string) error {
	return dbusServiceAction(ctx, "restart", resolveUnitName("wireguard", interfaceName))
}

func (e *LocalExecutor) ListWireGuardInterfaces(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "wg", "show", "interfaces").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("wg show interfaces failed: %v, output: %s", err, string(out))
	}
	return parseWireGuardInterfaces(out), nil
}

func (e *LocalExecutor) EnableWireGuardInterface(ctx context.Context, interfaceName string) error {
	return dbusServiceAction(ctx, "start", resolveUnitName("wireguard", interfaceName))
}

func (e *LocalExecutor) DisableWireGuardInterface(ctx context.Context, interfaceName string) error {
	return dbusServiceAction(ctx, "stop", resolveUnitName("wireguard", interfaceName))
}

func (e *LocalExecutor) Close() error {
	return nil
}

// --- Helpers ---

// dbusServiceAction executes a systemd unit action (start/stop/restart) via D-Bus.
func dbusServiceAction(ctx context.Context, action, service string) error {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return fmt.Errorf("D-Bus connect: %v", err)
	}
	defer conn.Close()

	unit := resolveUnitName(service)
	ch := make(chan string, 1)

	var opErr error
	switch action {
	case "restart":
		_, opErr = conn.RestartUnitContext(ctx, unit, "replace", ch)
	case "start":
		_, opErr = conn.StartUnitContext(ctx, unit, "replace", ch)
	case "stop":
		_, opErr = conn.StopUnitContext(ctx, unit, "replace", ch)
	default:
		return fmt.Errorf("unknown systemctl action: %s", action)
	}
	if opErr != nil {
		return fmt.Errorf("systemctl %s %s: %v", action, unit, opErr)
	}

	select {
	case result := <-ch:
		if result != "done" {
			return fmt.Errorf("systemctl %s %s: job result=%s", action, unit, result)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func resolveUnitName(service string, interfaceName ...string) string {
	if service == "wireguard" {
		iface := "wg0"
		if len(interfaceName) > 0 {
			iface = normalizeWireGuardInterfaceName(interfaceName[0])
		}
		return fmt.Sprintf("wg-quick@%s", iface)
	}
	return service
}

func normalizeWireGuardInterfaceName(interfaceName string) string {
	iface := strings.TrimSpace(strings.TrimSuffix(interfaceName, ".conf"))
	if iface == "" {
		return "wg0"
	}
	return iface
}

func parseWireGuardInterfaces(out []byte) []string {
	names := strings.Fields(strings.TrimSpace(string(out)))
	if len(names) == 0 {
		return []string{}
	}
	sort.Strings(names)
	return names
}
