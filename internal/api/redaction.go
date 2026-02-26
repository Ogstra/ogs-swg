package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

const maskedValue = "********"

var iniSensitiveLine = regexp.MustCompile(`(?im)^(\s*(?:PrivateKey|PublicKey)\s*=\s*).*$`)

func shouldRedactConfigReadOnly(r *http.Request) bool {
	perms := getPermissions(r)
	return perms != nil && perms.CanReadConfig && !perms.CanWriteConfig
}

func shouldRedactWireGuardReadOnly(r *http.Request) bool {
	perms := getPermissions(r)
	return perms != nil && perms.CanReadWireguard && !perms.CanWriteWireguard
}

func redactSingboxJSON(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}

	redacted := redactJSONValue(v)
	out, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return raw
	}
	return string(out)
}

func redactJSONValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if isSensitiveJSONKey(k) {
				out[k] = maskLike(val)
				continue
			}
			out[k] = redactJSONValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = redactJSONValue(t[i])
		}
		return out
	default:
		return v
	}
}

func isSensitiveJSONKey(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "private_key", "public_key", "uuid", "short_id":
		return true
	default:
		return false
	}
}

func maskLike(v any) any {
	switch t := v.(type) {
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = maskedValue
		}
		return out
	default:
		return maskedValue
	}
}

func redactWireGuardConfigText(raw string) string {
	return iniSensitiveLine.ReplaceAllString(raw, "${1}"+maskedValue)
}

func redactWireGuardInterfaceSecret(iface *core.WireGuardInterface) {
	if iface == nil {
		return
	}
	if strings.TrimSpace(iface.PrivateKey) != "" {
		iface.PrivateKey = maskedValue
	}
	if strings.TrimSpace(iface.PublicKey) != "" {
		iface.PublicKey = maskedValue
	}
}
