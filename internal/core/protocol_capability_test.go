package core

import "testing"

func TestCapabilityForCredentials(t *testing.T) {
	cases := []struct {
		protocolType string
		credential   CredentialKind
	}{
		{"vless", CredentialUUID},
		{"vmess", CredentialIDOrUUID},
		{"trojan", CredentialPasswordAsUUID},
		{"hysteria2", CredentialPassword},
		{"shadowsocks", CredentialPassword},
		{"anytls", CredentialPassword},
		{"naive", CredentialPassword},
	}

	for _, c := range cases {
		cap, ok := CapabilityFor(c.protocolType)
		if !ok {
			t.Errorf("CapabilityFor(%q) ok = false, want true", c.protocolType)
			continue
		}
		if cap.Credential != c.credential {
			t.Errorf("CapabilityFor(%q).Credential = %q, want %q", c.protocolType, cap.Credential, c.credential)
		}
	}
}

func TestCapabilityForFlags(t *testing.T) {
	vless, ok := CapabilityFor("vless")
	if !ok {
		t.Fatal("CapabilityFor(vless) ok = false")
	}
	if !vless.SupportsFlow {
		t.Error("vless.SupportsFlow = false, want true")
	}
	if !vless.SupportsReality {
		t.Error("vless.SupportsReality = false, want true")
	}

	vmess, ok := CapabilityFor("vmess")
	if !ok {
		t.Fatal("CapabilityFor(vmess) ok = false")
	}
	if !vmess.SupportsVmessFields {
		t.Error("vmess.SupportsVmessFields = false, want true")
	}
}

func TestCapabilityForUnknownTypes(t *testing.T) {
	unknown := []string{"socks", "VLESS", ""}
	for _, ut := range unknown {
		if _, ok := CapabilityFor(ut); ok {
			t.Errorf("CapabilityFor(%q) ok = true, want false", ut)
		}
	}
}

func TestCapabilityForReturnsCopy(t *testing.T) {
	cap1, ok := CapabilityFor("vless")
	if !ok {
		t.Fatal("CapabilityFor(vless) ok = false")
	}
	cap1.SupportsFlow = false
	cap1.Credential = CredentialPassword

	cap2, ok := CapabilityFor("vless")
	if !ok {
		t.Fatal("CapabilityFor(vless) ok = false")
	}
	if !cap2.SupportsFlow {
		t.Error("mutating returned copy affected subsequent lookup: SupportsFlow")
	}
	if cap2.Credential != CredentialUUID {
		t.Error("mutating returned copy affected subsequent lookup: Credential")
	}
}
