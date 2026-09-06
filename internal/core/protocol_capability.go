package core

// ProtocolType is a managed sing-box inbound protocol identifier, matching the
// lowercase value of an inbound's "type" key.
type ProtocolType string

const (
	ProtocolVLESS       ProtocolType = "vless"
	ProtocolVMess       ProtocolType = "vmess"
	ProtocolTrojan      ProtocolType = "trojan"
	ProtocolHysteria2   ProtocolType = "hysteria2"
	ProtocolShadowsocks ProtocolType = "shadowsocks"
	ProtocolAnyTLS      ProtocolType = "anytls"
	ProtocolNaive       ProtocolType = "naive"
)

// CredentialKind describes which per-user field carries the protocol secret and
// how GetUserInbounds must surface it on UserInboundInfo.
type CredentialKind string

const (
	CredentialUUID           CredentialKind = "uuid"             // vless: users[].uuid -> UserInboundInfo.UUID
	CredentialIDOrUUID       CredentialKind = "id_or_uuid"       // vmess: users[].uuid, falling back to users[].id -> UUID
	CredentialPasswordAsUUID CredentialKind = "password_as_uuid" // trojan: users[].password -> UUID
	CredentialPassword       CredentialKind = "password"         // hysteria2/shadowsocks/anytls/naive: users[].password -> Password
)

// ProtocolCapability is the authoritative description of one protocol's
// credential shape and supported inbound/link options. It is consumed by
// GetUserInbounds, the Go link builders, and (mirrored) the frontend inbound
// visibility logic.
type ProtocolCapability struct {
	Type                ProtocolType
	Credential          CredentialKind
	SupportsFlow        bool // vless flow parameter on users and links
	SupportsVmessFields bool // users[].security / users[].alterId
	SupportsTLS         bool // inbound has a tls block
	SupportsReality     bool // tls.reality is meaningful
	SupportsALPN        bool // tls.alpn is emitted on share links / editable
	SupportsTransport   bool // inbound has a transport block
	SupportsMultiplex   bool // inbound has a multiplex block
	SupportsBandwidth   bool // hysteria2 up_mbps/down_mbps and related options
	SupportsObfs        bool // hysteria2 obfs block
	SupportsMethod      bool // shadowsocks method
	SupportsServerKey   bool // shadowsocks inbound-level server key (top-level "password")
	SupportsNetwork     bool // top-level "network" key (shadowsocks, naive)
}

// protocolCapabilities is the single typed source of truth for per-protocol
// credential and option semantics. Do not replace with an untyped map value
// type; see D-01 in phase 54's context.
var protocolCapabilities = map[ProtocolType]ProtocolCapability{
	ProtocolVLESS: {
		Type:              ProtocolVLESS,
		Credential:        CredentialUUID,
		SupportsFlow:      true,
		SupportsTLS:       true,
		SupportsReality:   true,
		SupportsALPN:      true,
		SupportsTransport: true,
		SupportsMultiplex: true,
	},
	ProtocolVMess: {
		Type:                ProtocolVMess,
		Credential:          CredentialIDOrUUID,
		SupportsVmessFields: true,
		SupportsTLS:         true,
		SupportsALPN:        true,
		SupportsTransport:   true,
		SupportsMultiplex:   true,
	},
	ProtocolTrojan: {
		Type:       ProtocolTrojan,
		Credential: CredentialPasswordAsUUID,
		// GetUserInbounds currently copies user.Security and user.AlterID onto
		// trojan entries too (falls through to the shared append path). This
		// flag records that existing behavior so 54-03 stays byte-identical.
		SupportsVmessFields: true,
		SupportsTLS:         true,
		SupportsALPN:        true,
		SupportsTransport:   true,
		SupportsMultiplex:   true,
	},
	ProtocolHysteria2: {
		Type:              ProtocolHysteria2,
		Credential:        CredentialPassword,
		SupportsTLS:       true,
		SupportsBandwidth: true,
		SupportsObfs:      true,
	},
	ProtocolShadowsocks: {
		Type:              ProtocolShadowsocks,
		Credential:        CredentialPassword,
		SupportsMultiplex: true,
		SupportsMethod:    true,
		SupportsServerKey: true,
		SupportsNetwork:   true,
	},
	ProtocolAnyTLS: {
		Type:         ProtocolAnyTLS,
		Credential:   CredentialPassword,
		SupportsTLS:  true,
		SupportsALPN: true,
	},
	ProtocolNaive: {
		Type:            ProtocolNaive,
		Credential:      CredentialPassword,
		SupportsTLS:     true,
		SupportsNetwork: true,
	},
}

// CapabilityFor returns the descriptor for an inbound type string. The lookup is
// exact (no case folding) so it matches the existing switch statements on the raw
// inbound "type" value; unknown/unmanaged types return ok == false.
func CapabilityFor(inboundType string) (ProtocolCapability, bool) {
	cap, ok := protocolCapabilities[ProtocolType(inboundType)]
	return cap, ok
}
