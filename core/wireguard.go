package core

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type WireGuardPeer struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key,omitempty"`
	AllowedIPs string `json:"allowed_ips"`
	Endpoint   string `json:"endpoint,omitempty"`
	Alias      string `json:"alias,omitempty"`
	Email               string `json:"email,omitempty"`
	PresharedKey        string `json:"preshared_key,omitempty"`
	PersistentKeepalive int    `json:"persistent_keepalive,omitempty"`
}

type WireGuardInterface struct {
	Address    string `json:"address"`
	PrivateKey string `json:"private_key"`
	ListenPort int    `json:"listen_port"`
	PostUp     string `json:"post_up,omitempty"`
	PostDown   string `json:"post_down,omitempty"`
	MTU        int    `json:"mtu,omitempty"`
	DNS        string `json:"dns,omitempty"`
	PublicKey  string `json:"public_key,omitempty"`
}

type WireGuardConfig struct {
	Interface WireGuardInterface
	Peers     []WireGuardPeer
	Path      string
}

func applyPeerMetadata(comment string, peer *WireGuardPeer) {
	if peer == nil {
		return
	}
	parts := strings.SplitN(comment, "=", 2)
	if len(parts) != 2 {
		return
	}
	key := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	switch key {
	case "alias":
		peer.Alias = value
	case "email":
		peer.Alias = value
		peer.Email = value
	}
}

func GenerateWireGuardKeys() (string, string, error) {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", "", err
	}
	return key.String(), key.PublicKey().String(), nil
}

func LoadWireGuardConfig(path string) (*WireGuardConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &WireGuardConfig{Path: path}, nil
		}
		return nil, err
	}
	defer file.Close()

	config := &WireGuardConfig{Path: path}
	scanner := bufio.NewScanner(file)

	var currentSection string
	var currentPeer *WireGuardPeer

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			if currentSection == "peer" && currentPeer != nil {
				comment := strings.TrimSpace(line[1:])
				applyPeerMetadata(comment, currentPeer)
			}
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(line[1 : len(line)-1])
			if currentSection == "peer" {
				if currentPeer != nil {
					config.Peers = append(config.Peers, *currentPeer)
				}
				currentPeer = &WireGuardPeer{}
			}
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 && currentSection != "peer" {
			continue
		}

		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if idx := strings.Index(value, "#"); idx != -1 {
				comment := strings.TrimSpace(value[idx+1:])
				if currentSection == "peer" && currentPeer != nil {
					applyPeerMetadata(comment, currentPeer)
				}
				value = strings.TrimSpace(value[:idx])
			}

			switch currentSection {
			case "interface":
				switch strings.ToLower(key) {
				case "address":
					config.Interface.Address = value
				case "privatekey":
					config.Interface.PrivateKey = value
				case "listenport":
					port, _ := strconv.Atoi(value)
					config.Interface.ListenPort = port
				case "postup":
					config.Interface.PostUp = value
				case "postdown":
					config.Interface.PostDown = value
				case "mtu":
					mtu, _ := strconv.Atoi(value)
					config.Interface.MTU = mtu
				case "dns":
					config.Interface.DNS = value
				}
			case "peer":
				if currentPeer != nil {
					switch strings.ToLower(key) {
					case "publickey":
						currentPeer.PublicKey = value
					case "allowedips":
						currentPeer.AllowedIPs = value
					case "endpoint":
						currentPeer.Endpoint = value
					case "presharedkey":
						currentPeer.PresharedKey = value
					case "persistentkeepalive":
						pk, _ := strconv.Atoi(value)
						currentPeer.PersistentKeepalive = pk
					}
				}
			}
		}
	}

	if currentPeer != nil {
		config.Peers = append(config.Peers, *currentPeer)
	}

	if config.Interface.PrivateKey != "" {
		if pk, err := wgtypes.ParseKey(config.Interface.PrivateKey); err == nil {
			config.Interface.PublicKey = pk.PublicKey().String()
		}
	}

	return config, nil
}

func (c *WireGuardConfig) Save() error {
	f, err := os.Create(c.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintln(f, "[Interface]")
	if c.Interface.Address != "" {
		fmt.Fprintf(f, "Address = %s\n", c.Interface.Address)
	}
	if c.Interface.PrivateKey != "" {
		fmt.Fprintf(f, "PrivateKey = %s\n", c.Interface.PrivateKey)
	}
	if c.Interface.ListenPort != 0 {
		fmt.Fprintf(f, "ListenPort = %d\n", c.Interface.ListenPort)
	}
	if c.Interface.PostUp != "" {
		fmt.Fprintf(f, "PostUp = %s\n", c.Interface.PostUp)
	}
	if c.Interface.PostDown != "" {
		fmt.Fprintf(f, "PostDown = %s\n", c.Interface.PostDown)
	}
	if c.Interface.MTU != 0 {
		fmt.Fprintf(f, "MTU = %d\n", c.Interface.MTU)
	}
	if c.Interface.DNS != "" {
		fmt.Fprintf(f, "DNS = %s\n", c.Interface.DNS)
	}
	fmt.Fprintln(f, "")

	for _, peer := range c.Peers {
		fmt.Fprintln(f, "[Peer]")
		if peer.Alias != "" {
			fmt.Fprintf(f, "# Alias = %s\n", peer.Alias)
		} else if peer.Email != "" {
			fmt.Fprintf(f, "# Alias = %s\n", peer.Email)
		}
		fmt.Fprintf(f, "PublicKey = %s\n", peer.PublicKey)
		fmt.Fprintf(f, "AllowedIPs = %s\n", peer.AllowedIPs)
		if peer.Endpoint != "" {
			fmt.Fprintf(f, "Endpoint = %s\n", peer.Endpoint)
		}
		if peer.PresharedKey != "" {
			fmt.Fprintf(f, "PresharedKey = %s\n", peer.PresharedKey)
		}
		if peer.PersistentKeepalive != 0 {
			fmt.Fprintf(f, "PersistentKeepalive = %d\n", peer.PersistentKeepalive)
		}
		fmt.Fprintln(f, "")
	}

	return nil
}

func (c *WireGuardConfig) AddPeer(peer WireGuardPeer) error {
	for _, p := range c.Peers {
		if p.PublicKey == peer.PublicKey {
			return fmt.Errorf("peer with public key already exists")
		}
		if peer.Alias != "" && (p.Alias == peer.Alias || p.Email == peer.Alias) {
			return fmt.Errorf("peer with alias already exists")
		}
	}
	c.Peers = append(c.Peers, peer)
	return c.Save()
}

func (c *WireGuardConfig) RemovePeer(publicKey string) error {
	newPeers := []WireGuardPeer{}
	found := false
	for _, p := range c.Peers {
		if p.PublicKey == publicKey {
			found = true
			continue
		}
		newPeers = append(newPeers, p)
	}
	if !found {
		return fmt.Errorf("peer not found")
	}
	c.Peers = newPeers
	return c.Save()
}

type PeerStats struct {
	PublicKey       string `json:"public_key"`
	Endpoint        string `json:"endpoint"`
	LatestHandshake int64  `json:"latest_handshake"`
	TransferRx      int64  `json:"transfer_rx"`
	TransferTx      int64  `json:"transfer_tx"`
}

func GetWireGuardStats() (map[string]PeerStats, error) {
	stats := make(map[string]PeerStats)

	c, err := wgctrl.New()
	if err != nil {
		return stats, nil
	}
	defer c.Close()

	devices, err := c.Devices()
	if err != nil {
		return stats, nil
	}

	for _, dev := range devices {
		for _, peer := range dev.Peers {
			endpoint := ""
			if peer.Endpoint != nil {
				endpoint = peer.Endpoint.String()
			}

			stats[peer.PublicKey.String()] = PeerStats{
				PublicKey:       peer.PublicKey.String(),
				Endpoint:        endpoint,
				LatestHandshake: peer.LastHandshakeTime.Unix(),
				TransferRx:      peer.ReceiveBytes,
				TransferTx:      peer.TransmitBytes,
			}
		}
	}

	return stats, nil
}

func (c *WireGuardConfig) UpdateInterface(updated WireGuardInterface) error {
	c.Interface = updated
	return c.Save()
}

func (c *WireGuardConfig) UpdatePeer(publicKey string, updated WireGuardPeer) error {
	for i, p := range c.Peers {
		if p.PublicKey == publicKey {
			c.Peers[i].AllowedIPs = updated.AllowedIPs
			c.Peers[i].Endpoint = updated.Endpoint
			c.Peers[i].PresharedKey = updated.PresharedKey
			c.Peers[i].PersistentKeepalive = updated.PersistentKeepalive
			if updated.Alias != "" {
				c.Peers[i].Alias = updated.Alias
			}
			return c.Save()
		}
	}
	return fmt.Errorf("peer not found")
}
