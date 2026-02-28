// Package core - sing-box typed struct definitions.
// Source: https://sing-box.sagernet.org/configuration/
// Generated for Phase 1 of the type safety refactor.
// DO NOT add map[string]interface{} or interface{} fields to this file.
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

// VlessUser is the user entry in a VLESS inbound.
// Source: https://sing-box.sagernet.org/configuration/inbound/vless/
type VlessUser struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	Flow string `json:"flow,omitempty"` // "xtls-rprx-vision" or empty
}

// VmessUser is the user entry in a VMess inbound.
// Source: https://sing-box.sagernet.org/configuration/inbound/vmess/
// NOTE: alterId uses camelCase JSON key per current sing-box docs.
// The existing code reads "alter_id" (snake_case) - that is a Phase 3 migration concern.
type VmessUser struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	AlterID int    `json:"alterId,omitempty"`
}

// TrojanUser is the user entry in a Trojan inbound.
// Source: https://sing-box.sagernet.org/configuration/inbound/trojan/
type TrojanUser struct {
	Name     string `json:"name"`
	Password string `json:"password"` // NOT uuid - Trojan uses password
}

// VlessInbound is a typed VLESS inbound configuration.
type VlessInbound struct {
	InboundBase
	ListenFields
	Users     []VlessUser     `json:"users"`
	TLS       json.RawMessage `json:"tls,omitempty"`
	Multiplex json.RawMessage `json:"multiplex,omitempty"`
	Transport json.RawMessage `json:"transport,omitempty"`
}

// VmessInbound is a typed VMess inbound configuration.
type VmessInbound struct {
	InboundBase
	ListenFields
	Users     []VmessUser     `json:"users"`
	TLS       json.RawMessage `json:"tls,omitempty"`
	Multiplex json.RawMessage `json:"multiplex,omitempty"`
	Transport json.RawMessage `json:"transport,omitempty"`
}

// TrojanInbound is a typed Trojan inbound configuration.
type TrojanInbound struct {
	InboundBase
	ListenFields
	Users     []TrojanUser    `json:"users"`
	TLS       json.RawMessage `json:"tls,omitempty"`
	Multiplex json.RawMessage `json:"multiplex,omitempty"`
	Transport json.RawMessage `json:"transport,omitempty"`
}

// V2RayStats is the stats sub-configuration within V2RayAPI.
// Source: https://sing-box.sagernet.org/configuration/experimental/v2ray-api/
type V2RayStats struct {
	Enabled   bool     `json:"enabled"` // NO omitempty - must always be written explicitly
	Inbounds  []string `json:"inbounds,omitempty"`
	Outbounds []string `json:"outbounds,omitempty"`
	Users     []string `json:"users,omitempty"`
}

// V2RayAPI is the experimental.v2ray_api configuration.
type V2RayAPI struct {
	Listen string      `json:"listen,omitempty"`
	Stats  *V2RayStats `json:"stats,omitempty"`
}

// Experimental is the top-level experimental configuration block.
// Only v2ray_api is typed; other experimental features use RawMessage.
type Experimental struct {
	V2RayAPI  *V2RayAPI       `json:"v2ray_api,omitempty"`
	ClashAPI  json.RawMessage `json:"clash_api,omitempty"`
	CacheFile json.RawMessage `json:"cache_file,omitempty"`
}

// SingboxConfig is the top-level sing-box configuration envelope.
// Source: https://sing-box.sagernet.org/configuration/
// Inbounds and Experimental are typed; all other sections are opaque RawMessage
// to guarantee round-trip fidelity (no data loss for fields the panel doesn't own).
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
