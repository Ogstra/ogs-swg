package core

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

// TestSingboxConfigCache_RepeatReadSkipsDiskAndParse verifies that a second
// read of an unchanged file performs no additional disk read or JSON parse.
func TestSingboxConfigCache_RepeatReadSkipsDiskAndParse(t *testing.T) {
	cfg, _ := newFileBackedTestConfig(t, configReadFixtureJSON)

	if _, err := cfg.GetSingboxConfigMap(); err != nil {
		t.Fatalf("first GetSingboxConfigMap: %v", err)
	}
	if _, err := cfg.GetSingboxConfigMap(); err != nil {
		t.Fatalf("second GetSingboxConfigMap: %v", err)
	}

	diskReads, jsonParses := cfg.singboxCacheStats()
	if diskReads != 1 {
		t.Errorf("diskReads = %d, want 1", diskReads)
	}
	if jsonParses != 1 {
		t.Errorf("jsonParses = %d, want 1", jsonParses)
	}
}

// TestSingboxConfigCache_ModTimeChangeInvalidates verifies that rewriting the
// file with a new mod time forces a re-read and re-parse, returning fresh
// content.
func TestSingboxConfigCache_ModTimeChangeInvalidates(t *testing.T) {
	cfg, path := newFileBackedTestConfig(t, configReadFixtureJSON)

	if _, err := cfg.GetSingboxConfigMap(); err != nil {
		t.Fatalf("first GetSingboxConfigMap: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	modified := `{"log": {"level": "debug"}, "inbounds": [], "outbounds": []}`
	if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}

	got, err := cfg.GetSingboxConfigMap()
	if err != nil {
		t.Fatalf("third GetSingboxConfigMap: %v", err)
	}

	diskReads, jsonParses := cfg.singboxCacheStats()
	if diskReads != 2 {
		t.Errorf("diskReads = %d, want 2", diskReads)
	}
	if jsonParses != 2 {
		t.Errorf("jsonParses = %d, want 2", jsonParses)
	}

	logSection, _ := got["log"].(map[string]interface{})
	if logSection == nil || logSection["level"] != "debug" {
		t.Errorf("expected new content to be returned, got %+v", got)
	}
}

// TestSingboxConfigCache_SizeChangeInvalidates verifies that a size change
// with a pinned identical mod time still invalidates the cache.
func TestSingboxConfigCache_SizeChangeInvalidates(t *testing.T) {
	cfg, path := newFileBackedTestConfig(t, configReadFixtureJSON)

	if _, err := cfg.GetSingboxConfigMap(); err != nil {
		t.Fatalf("first GetSingboxConfigMap: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	pinnedTime := info.ModTime()

	modified := `{"log": {"level": "debug_with_more_characters_to_change_size"}, "inbounds": [], "outbounds": []}`
	if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}
	if err := os.Chtimes(path, pinnedTime, pinnedTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	got, err := cfg.GetSingboxConfigMap()
	if err != nil {
		t.Fatalf("second GetSingboxConfigMap: %v", err)
	}

	diskReads, jsonParses := cfg.singboxCacheStats()
	if diskReads != 2 {
		t.Errorf("diskReads = %d, want 2", diskReads)
	}
	if jsonParses != 2 {
		t.Errorf("jsonParses = %d, want 2", jsonParses)
	}

	logSection, _ := got["log"].(map[string]interface{})
	if logSection == nil || logSection["level"] != "debug_with_more_characters_to_change_size" {
		t.Errorf("expected new content to be returned, got %+v", got)
	}
}

// TestSingboxConfigCache_WriteInvalidates verifies that a write through
// UpdateSingboxConfig forces the next read to re-read and return fresh
// content.
func TestSingboxConfigCache_WriteInvalidates(t *testing.T) {
	cfg, _ := newFileBackedTestConfig(t, configReadFixtureJSON)

	if _, err := cfg.GetSingboxConfigMap(); err != nil {
		t.Fatalf("first GetSingboxConfigMap: %v", err)
	}

	beforeReads, _ := cfg.singboxCacheStats()

	newJSON := `{"log": {"level": "written"}, "inbounds": [], "outbounds": []}`
	if err := cfg.UpdateSingboxConfig(newJSON); err != nil {
		t.Fatalf("UpdateSingboxConfig: %v", err)
	}

	got, err := cfg.GetSingboxConfigMap()
	if err != nil {
		t.Fatalf("GetSingboxConfigMap after write: %v", err)
	}

	afterReads, _ := cfg.singboxCacheStats()
	if afterReads <= beforeReads {
		t.Errorf("expected disk reads to increase after write, before=%d after=%d", beforeReads, afterReads)
	}

	logSection, _ := got["log"].(map[string]interface{})
	if logSection == nil || logSection["level"] != "written" {
		t.Errorf("expected written content to be returned, got %+v", got)
	}
}

// TestSingboxConfigCache_MutationIsolation verifies that mutating structures
// returned from the cached read path cannot corrupt what the next caller
// receives.
func TestSingboxConfigCache_MutationIsolation(t *testing.T) {
	cfg, _ := newFileBackedTestConfig(t, configReadFixtureJSON)

	first, err := cfg.GetSingboxConfigMap()
	if err != nil {
		t.Fatalf("first GetSingboxConfigMap: %v", err)
	}
	firstBytes, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}

	// Mutate the returned map aggressively.
	delete(first, "log")
	if inbounds, ok := first["inbounds"].([]interface{}); ok && len(inbounds) > 0 {
		if inb, ok := inbounds[0].(map[string]interface{}); ok {
			inb["listen_port"] = 99999
		}
	}

	view, err := cfg.GetSingboxInboundView("gold-hy2")
	if err != nil {
		t.Fatalf("GetSingboxInboundView: %v", err)
	}
	if view.Raw != nil {
		view.Raw["mutated"] = true
	}
	if view.TLS != nil && view.TLS.ALPN != nil {
		view.TLS.ALPN[0] = "mutated-alpn"
	}

	second, err := cfg.GetSingboxConfigMap()
	if err != nil {
		t.Fatalf("second GetSingboxConfigMap: %v", err)
	}
	secondBytes, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}

	if string(firstBytes) != string(secondBytes) {
		t.Errorf("mutation of returned map leaked into next call:\nfirst:  %s\nsecond: %s", firstBytes, secondBytes)
	}

	view2, err := cfg.GetSingboxInboundView("gold-hy2")
	if err != nil {
		t.Fatalf("GetSingboxInboundView second: %v", err)
	}
	if view2.Raw != nil {
		if _, mutated := view2.Raw["mutated"]; mutated {
			t.Errorf("Raw map mutation leaked into next call")
		}
	}
}

// TestSingboxConfigCache_NoStatFallback verifies that when the config path
// cannot be stat'd, the cache is fully disabled and behavior matches
// pre-refactor: every call increments both counters.
func TestSingboxConfigCache_NoStatFallback(t *testing.T) {
	cfg, _ := newTestConfig(t, configReadFixtureJSON)

	if _, err := cfg.GetSingboxConfigMap(); err != nil {
		t.Fatalf("first GetSingboxConfigMap: %v", err)
	}
	if _, err := cfg.GetSingboxConfigMap(); err != nil {
		t.Fatalf("second GetSingboxConfigMap: %v", err)
	}

	diskReads, jsonParses := cfg.singboxCacheStats()
	if diskReads != 2 {
		t.Errorf("diskReads = %d, want 2 (cache disabled)", diskReads)
	}
	if jsonParses != 2 {
		t.Errorf("jsonParses = %d, want 2 (cache disabled)", jsonParses)
	}

	newJSON := `{"log": {"level": "written"}, "inbounds": [], "outbounds": []}`
	if err := cfg.UpdateSingboxConfig(newJSON); err != nil {
		t.Fatalf("UpdateSingboxConfig: %v", err)
	}
	got, err := cfg.GetSingboxConfigMap()
	if err != nil {
		t.Fatalf("GetSingboxConfigMap after write: %v", err)
	}
	logSection, _ := got["log"].(map[string]interface{})
	if logSection == nil || logSection["level"] != "written" {
		t.Errorf("expected written content to be returned, got %+v", got)
	}

	diskReads, jsonParses = cfg.singboxCacheStats()
	if diskReads != 3 {
		t.Errorf("diskReads = %d, want 3 (cache disabled)", diskReads)
	}
	if jsonParses != 3 {
		t.Errorf("jsonParses = %d, want 3 (cache disabled)", jsonParses)
	}
}

// TestSingboxConfigCache_ViewsCachedAndCloned verifies that two consecutive
// GetSingboxInboundViews calls report a single parse and return deep-equal
// but non-aliased slices.
func TestSingboxConfigCache_ViewsCachedAndCloned(t *testing.T) {
	cfg, _ := newFileBackedTestConfig(t, configReadFixtureJSON)

	first, err := cfg.GetSingboxInboundViews()
	if err != nil {
		t.Fatalf("first GetSingboxInboundViews: %v", err)
	}
	second, err := cfg.GetSingboxInboundViews()
	if err != nil {
		t.Fatalf("second GetSingboxInboundViews: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Errorf("expected deep-equal view slices")
	}

	for i := range first {
		if first[i].Raw == nil || second[i].Raw == nil {
			continue
		}
		if reflect.ValueOf(first[i].Raw).Pointer() == reflect.ValueOf(second[i].Raw).Pointer() {
			t.Errorf("view %d Raw map shares identity between calls", i)
		}
	}
}
