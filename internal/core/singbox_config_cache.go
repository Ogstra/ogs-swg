package core

import (
	"os"
	"time"
)

// singboxConfigCache memoizes the most recent successful read of
// SingboxConfigPath, keyed by the file's mod time and size. Every field is
// read and written under Config.mu, so the struct carries no lock of its own.
type singboxConfigCache struct {
	path    string
	modTime time.Time
	size    int64
	raw     []byte                 // exact bytes last read from disk
	parsed  map[string]interface{} // lazily filled top-level JSON object
	views   []SingboxInboundView   // lazily filled decoded inbound views
}

// statSingboxConfigLocked reports the current mod time and size of the config
// file. ok is false when the path cannot be stat'd (in-memory executors, tests,
// remote paths); callers must then bypass the cache entirely and use the
// pre-cache read path unchanged.
func (c *Config) statSingboxConfigLocked() (modTime time.Time, size int64, ok bool) {
	info, err := os.Stat(c.SingboxConfigPath)
	if err != nil {
		return time.Time{}, 0, false
	}
	return info.ModTime(), info.Size(), true
}

// singboxCacheValidLocked reports whether the memoized entry still matches the
// on-disk file identity.
func (c *Config) singboxCacheValidLocked(modTime time.Time, size int64) bool {
	if c.singboxCache == nil {
		return false
	}
	if c.singboxCache.path != c.SingboxConfigPath {
		return false
	}
	return c.singboxCache.modTime.Equal(modTime) && c.singboxCache.size == size
}

// invalidateSingboxConfigCacheLocked drops the memoized parse. It must be called
// from every code path that writes SingboxConfigPath.
func (c *Config) invalidateSingboxConfigCacheLocked() {
	c.singboxCache = nil
}

// singboxCacheStats returns the number of disk reads and JSON parses performed
// by the sing-box config read path. Test instrumentation only.
func (c *Config) singboxCacheStats() (diskReads, jsonParses int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.singboxDiskReads, c.singboxJSONParses
}

// deepCopyJSONValue recursively deep-copies a decoded JSON value so that the
// caller cannot mutate anything reachable from the cached tree.
func deepCopyJSONValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return deepCopyJSONMap(val)
	case []interface{}:
		if val == nil {
			return nil
		}
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = deepCopyJSONValue(item)
		}
		return out
	default:
		return val
	}
}

// deepCopyJSONMap deep-copies a decoded JSON object. Nil maps stay nil; empty
// maps stay empty (never collapse {} to nil, that would change golden output).
func deepCopyJSONMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = deepCopyJSONValue(v)
	}
	return out
}

// cloneSingboxInboundView returns a deep copy of v so that a caller mutating
// the returned view cannot corrupt the cached copy or other callers' copies.
func cloneSingboxInboundView(v SingboxInboundView) SingboxInboundView {
	out := v

	if v.Users != nil {
		out.Users = append([]SingboxInboundUserView(nil), v.Users...)
	}

	if v.TLS != nil {
		tls := *v.TLS
		if v.TLS.ALPN != nil {
			tls.ALPN = append([]string(nil), v.TLS.ALPN...)
		}
		if v.TLS.Reality != nil {
			reality := *v.TLS.Reality
			if v.TLS.Reality.ShortIDs != nil {
				reality.ShortIDs = append([]string(nil), v.TLS.Reality.ShortIDs...)
			}
			tls.Reality = &reality
		}
		out.TLS = &tls
	}

	if v.Obfs != nil {
		obfs := *v.Obfs
		out.Obfs = &obfs
	}

	out.Raw = deepCopyJSONMap(v.Raw)

	return out
}

// cloneSingboxInboundViews deep-copies a slice of inbound views.
func cloneSingboxInboundViews(in []SingboxInboundView) []SingboxInboundView {
	if in == nil {
		return nil
	}
	out := make([]SingboxInboundView, len(in))
	for i, v := range in {
		out[i] = cloneSingboxInboundView(v)
	}
	return out
}
