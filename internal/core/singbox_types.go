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

type Experimental struct {
	V2RayAPI  *V2RayAPI       `json:"v2ray_api,omitempty"`
	ClashAPI  json.RawMessage `json:"clash_api,omitempty"`
	CacheFile json.RawMessage `json:"cache_file,omitempty"`
}

type SingboxConfig struct {
	Log          json.RawMessage `json:"log,omitempty"`
	DNS          json.RawMessage `json:"dns,omitempty"`
	NTP          json.RawMessage `json:"ntp,omitempty"`
	Certificate  json.RawMessage `json:"certificate,omitempty"`
	Endpoints    json.RawMessage `json:"endpoints,omitempty"`
	Inbounds     json.RawMessage `json:"inbounds,omitempty"` // typed per-inbound in Phase 2
	Outbounds    json.RawMessage `json:"outbounds,omitempty"`
	Route        json.RawMessage `json:"route,omitempty"`
	Services     json.RawMessage `json:"services,omitempty"`
	Experimental *Experimental   `json:"experimental,omitempty"`
}
