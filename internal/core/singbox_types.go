package core

import "encoding/json"

type InboundBase struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}

type ListenFields struct {
	Listen               string          `json:"listen,omitempty"`
	ListenPort           int             `json:"listen_port,omitempty"`
	BindInterface        string          `json:"bind_interface,omitempty"`
	RoutingMark          json.RawMessage `json:"routing_mark,omitempty"` // int | "0xHEX" union type
	ReuseAddr            bool            `json:"reuse_addr,omitempty"`
	Netns                string          `json:"netns,omitempty"`
	TCPFastOpen          bool            `json:"tcp_fast_open,omitempty"`
	TCPMultiPath         bool            `json:"tcp_multi_path,omitempty"`
	DisableTCPKeepAlive  bool            `json:"disable_tcp_keep_alive,omitempty"`
	TCPKeepAlive         string          `json:"tcp_keep_alive,omitempty"`
	TCPKeepAliveInterval string          `json:"tcp_keep_alive_interval,omitempty"`
	UDPFragment          bool            `json:"udp_fragment,omitempty"`
	UDPTimeout           string          `json:"udp_timeout,omitempty"`
	Detour               string          `json:"detour,omitempty"`
}

type VlessUser struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	Flow string `json:"flow,omitempty"` // "xtls-rprx-vision" or empty
}

type VmessUser struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	AlterID int    `json:"alterId,omitempty"`
}

type TrojanUser struct {
	Name     string `json:"name"`
	Password string `json:"password"` // NOT uuid - Trojan uses password
}

type AnyTLSUser struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type NaiveUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ShadowsocksUser struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type VlessInbound struct {
	InboundBase
	ListenFields
	Users     []VlessUser     `json:"users"`
	TLS       json.RawMessage `json:"tls,omitempty"`
	Multiplex json.RawMessage `json:"multiplex,omitempty"`
	Transport json.RawMessage `json:"transport,omitempty"`
}

type VmessInbound struct {
	InboundBase
	ListenFields
	Users     []VmessUser     `json:"users"`
	TLS       json.RawMessage `json:"tls,omitempty"`
	Multiplex json.RawMessage `json:"multiplex,omitempty"`
	Transport json.RawMessage `json:"transport,omitempty"`
}

type TrojanInbound struct {
	InboundBase
	ListenFields
	Users     []TrojanUser    `json:"users"`
	TLS       json.RawMessage `json:"tls,omitempty"`
	Multiplex json.RawMessage `json:"multiplex,omitempty"`
	Transport json.RawMessage `json:"transport,omitempty"`
}

type AnyTLSInbound struct {
	InboundBase
	ListenFields
	Users         []AnyTLSUser    `json:"users"`
	PaddingScheme []string        `json:"padding_scheme,omitempty"`
	TLS           json.RawMessage `json:"tls,omitempty"`
}

type NaiveInbound struct {
	InboundBase
	ListenFields
	Network               string          `json:"network,omitempty"`
	Users                 []NaiveUser     `json:"users"`
	QuicCongestionControl string          `json:"quic_congestion_control,omitempty"`
	TLS                   json.RawMessage `json:"tls,omitempty"`
}

type ShadowsocksInbound struct {
	InboundBase
	ListenFields
	Network   []string          `json:"network,omitempty"`
	Method    string            `json:"method"`
	Password  string            `json:"password,omitempty"`
	Users     []ShadowsocksUser `json:"users"`
	Multiplex json.RawMessage   `json:"multiplex,omitempty"`
}

type RealityHandshake struct {
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
}

type RealityConfig struct {
	Enabled    bool             `json:"enabled,omitempty"`
	Handshake  RealityHandshake `json:"handshake,omitempty"`
	PrivateKey string           `json:"private_key,omitempty"`
	PublicKey  string           `json:"public_key,omitempty"`
	ShortIDs   []string         `json:"short_id,omitempty"`
}

func (r *RealityConfig) UnmarshalJSON(data []byte) error {
	type alias RealityConfig
	var tmp struct {
		alias
		RawShortID json.RawMessage `json:"short_id,omitempty"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*r = RealityConfig(tmp.alias)
	if len(tmp.RawShortID) == 0 || string(tmp.RawShortID) == "null" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(tmp.RawShortID, &arr); err == nil {
		r.ShortIDs = arr
		return nil
	}
	var s string
	if err := json.Unmarshal(tmp.RawShortID, &s); err == nil {
		r.ShortIDs = []string{s}
		return nil
	}
	return nil
}

type TLSConfig struct {
	Enabled         bool           `json:"enabled,omitempty"`
	ServerName      string         `json:"server_name,omitempty"`
	ALPN            []string       `json:"alpn,omitempty"`
	CertificatePath string         `json:"certificate_path,omitempty"`
	Reality         *RealityConfig `json:"reality,omitempty"`
}

type ManagedInbound interface {
	Base() InboundBase
	UserNames() []string
}

func (v *VlessInbound) Base() InboundBase { return v.InboundBase }

func (v *VlessInbound) UserNames() []string {
	names := make([]string, len(v.Users))
	for i, u := range v.Users {
		names[i] = u.Name
	}
	return names
}

func (v *VmessInbound) Base() InboundBase { return v.InboundBase }

func (v *VmessInbound) UserNames() []string {
	names := make([]string, len(v.Users))
	for i, u := range v.Users {
		names[i] = u.Name
	}
	return names
}

func (t *TrojanInbound) Base() InboundBase { return t.InboundBase }

func (t *TrojanInbound) UserNames() []string {
	names := make([]string, len(t.Users))
	for i, u := range t.Users {
		names[i] = u.Name
	}
	return names
}

func (a *AnyTLSInbound) Base() InboundBase { return a.InboundBase }

func (a *AnyTLSInbound) UserNames() []string {
	names := make([]string, len(a.Users))
	for i, u := range a.Users {
		names[i] = u.Name
	}
	return names
}

func (n *NaiveInbound) Base() InboundBase { return n.InboundBase }

func (n *NaiveInbound) UserNames() []string {
	names := make([]string, len(n.Users))
	for i, u := range n.Users {
		names[i] = u.Username
	}
	return names
}

func (s *ShadowsocksInbound) Base() InboundBase { return s.InboundBase }

func (s *ShadowsocksInbound) UserNames() []string {
	names := make([]string, len(s.Users))
	for i, u := range s.Users {
		names[i] = u.Name
	}
	return names
}

type Hysteria2User struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type Hysteria2Obfs struct {
	Type     string `json:"type"` // always "salamander" — no omitempty, must be present when block exists
	Password string `json:"password"`
}

type Hysteria2Inbound struct {
	InboundBase
	ListenFields
	UpMbps                int             `json:"up_mbps,omitempty"`
	DownMbps              int             `json:"down_mbps,omitempty"`
	Obfs                  *Hysteria2Obfs  `json:"obfs,omitempty"`
	Users                 []Hysteria2User `json:"users"`
	IgnoreClientBandwidth bool            `json:"ignore_client_bandwidth,omitempty"`
	TLS                   json.RawMessage `json:"tls,omitempty"`
	Masquerade            json.RawMessage `json:"masquerade,omitempty"`
	BBRProfile            string          `json:"bbr_profile,omitempty"`
}

func (h *Hysteria2Inbound) Base() InboundBase { return h.InboundBase }

func (h *Hysteria2Inbound) UserNames() []string {
	names := make([]string, len(h.Users))
	for i, u := range h.Users {
		names[i] = u.Name
	}
	return names
}

type V2RayStats struct {
	Enabled   bool     `json:"enabled"` // NO omitempty - must always be written explicitly
	Inbounds  []string `json:"inbounds,omitempty"`
	Outbounds []string `json:"outbounds,omitempty"`
	Users     []string `json:"users,omitempty"`
}

type V2RayAPI struct {
	Listen string      `json:"listen,omitempty"`
	Stats  *V2RayStats `json:"stats,omitempty"`
}

type ClashAPI struct {
	ExternalController    string                     `json:"external_controller,omitempty"`
	Secret                string                     `json:"secret,omitempty"`
	Extra                 map[string]json.RawMessage `json:"-"`
	hasExternalController bool
	hasSecret             bool
}

func (c *ClashAPI) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*c = ClashAPI{}
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var next ClashAPI
	if v, ok := raw["external_controller"]; ok {
		next.hasExternalController = true
		if err := json.Unmarshal(v, &next.ExternalController); err != nil {
			return err
		}
	}
	if v, ok := raw["secret"]; ok {
		next.hasSecret = true
		if err := json.Unmarshal(v, &next.Secret); err != nil {
			return err
		}
	}

	delete(raw, "external_controller")
	delete(raw, "secret")
	if len(raw) > 0 {
		next.Extra = make(map[string]json.RawMessage, len(raw))
		for key, value := range raw {
			next.Extra[key] = append(json.RawMessage(nil), value...)
		}
	}

	*c = next
	return nil
}

func (c ClashAPI) MarshalJSON() ([]byte, error) {
	raw := make(map[string]json.RawMessage, len(c.Extra)+2)
	for key, value := range c.Extra {
		raw[key] = append(json.RawMessage(nil), value...)
	}

	if c.hasExternalController || c.ExternalController != "" {
		data, err := json.Marshal(c.ExternalController)
		if err != nil {
			return nil, err
		}
		raw["external_controller"] = data
	}
	if c.hasSecret || c.Secret != "" {
		data, err := json.Marshal(c.Secret)
		if err != nil {
			return nil, err
		}
		raw["secret"] = data
	}

	return json.Marshal(raw)
}

type Experimental struct {
	V2RayAPI  *V2RayAPI                  `json:"v2ray_api,omitempty"`
	ClashAPI  *ClashAPI                  `json:"clash_api,omitempty"`
	CacheFile json.RawMessage            `json:"cache_file,omitempty"`
	Extra     map[string]json.RawMessage `json:"-"`
}

func (e *Experimental) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*e = Experimental{}
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var next Experimental
	if v, ok := raw["v2ray_api"]; ok && len(v) > 0 && string(v) != "null" {
		var api V2RayAPI
		if err := json.Unmarshal(v, &api); err != nil {
			return err
		}
		next.V2RayAPI = &api
	}
	if v, ok := raw["clash_api"]; ok && len(v) > 0 && string(v) != "null" {
		var api ClashAPI
		if err := json.Unmarshal(v, &api); err != nil {
			return err
		}
		next.ClashAPI = &api
	}
	if v, ok := raw["cache_file"]; ok {
		next.CacheFile = append(json.RawMessage(nil), v...)
	}

	delete(raw, "v2ray_api")
	delete(raw, "clash_api")
	delete(raw, "cache_file")
	if len(raw) > 0 {
		next.Extra = make(map[string]json.RawMessage, len(raw))
		for key, value := range raw {
			next.Extra[key] = append(json.RawMessage(nil), value...)
		}
	}

	*e = next
	return nil
}

func (e Experimental) MarshalJSON() ([]byte, error) {
	raw := make(map[string]json.RawMessage, len(e.Extra)+3)
	for key, value := range e.Extra {
		raw[key] = append(json.RawMessage(nil), value...)
	}

	if e.V2RayAPI != nil {
		data, err := json.Marshal(e.V2RayAPI)
		if err != nil {
			return nil, err
		}
		raw["v2ray_api"] = data
	}
	if e.ClashAPI != nil {
		data, err := json.Marshal(e.ClashAPI)
		if err != nil {
			return nil, err
		}
		raw["clash_api"] = data
	}
	if len(e.CacheFile) > 0 {
		raw["cache_file"] = append(json.RawMessage(nil), e.CacheFile...)
	}

	return json.Marshal(raw)
}

type SingboxConfig struct {
	Log          json.RawMessage `json:"log,omitempty"`
	DNS          json.RawMessage `json:"dns,omitempty"`
	NTP          json.RawMessage `json:"ntp,omitempty"`
	Certificate  json.RawMessage `json:"certificate,omitempty"`
	Endpoints    json.RawMessage `json:"endpoints,omitempty"`
	Inbounds     json.RawMessage `json:"inbounds,omitempty"`
	Outbounds    json.RawMessage `json:"outbounds,omitempty"`
	Route        json.RawMessage `json:"route,omitempty"`
	Services     json.RawMessage `json:"services,omitempty"`
	Experimental *Experimental   `json:"experimental,omitempty"`
}

type SingboxInboundMeta struct {
	Tag        string `json:"tag"`
	Type       string `json:"type"`
	ListenPort int    `json:"listen_port,omitempty"`
}

type SingboxInboundUserView struct {
	Name     string `json:"name,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	ID       string `json:"id,omitempty"`
	Password string `json:"password,omitempty"`
	Flow     string `json:"flow,omitempty"`
	Security string `json:"security,omitempty"`
	AlterID  int    `json:"alterId,omitempty"`
}

// SingboxObfsConfig is the typed form of a hysteria2 inbound's "obfs" block.
type SingboxObfsConfig struct {
	Type     string `json:"type,omitempty"`
	Password string `json:"password,omitempty"`
}

type SingboxInboundView struct {
	Tag        string                   `json:"tag"`
	Type       string                   `json:"type"`
	ListenPort int                      `json:"listen_port,omitempty"`
	Users      []SingboxInboundUserView `json:"users,omitempty"`
	TLS        *TLSConfig               `json:"-"`
	Obfs       *SingboxObfsConfig       `json:"-"` // hysteria2 obfs block, nil when absent
	Network    string                   `json:"-"` // top-level "network" (shadowsocks, naive), trimmed
	Method     string                   `json:"-"` // shadowsocks method, trimmed
	ServerKey  string                   `json:"-"` // shadowsocks inbound-level "password" (server key), trimmed
	Raw        map[string]interface{}   `json:"-"`
}

type SingboxOutboundView struct {
	Tag            string `json:"tag"`
	Type           string `json:"type"`
	Server         string `json:"server,omitempty"`
	ServerPort     int    `json:"server_port,omitempty"`
	DomainStrategy string `json:"domain_strategy,omitempty"`
	DomainResolver string `json:"domain_resolver,omitempty"`
}

type SingboxOutboundDomainStrategyUpdate struct {
	Tag            string `json:"tag"`
	DomainStrategy string `json:"domain_strategy,omitempty"`
}
