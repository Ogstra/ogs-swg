package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubscriptionHistoryRetention_IsOffUnlessChosen(t *testing.T) {
	srv, _ := newNtfyTestServerWithStore(t)
	if srv.config.SubscriptionHistoryRetentionDays != 0 {
		t.Fatalf("default retention = %d; nothing may be deleted unless it is chosen", srv.config.SubscriptionHistoryRetentionDays)
	}

	put := func(value any) {
		body, _ := json.Marshal(map[string]any{"subscription_history_retention_days": value})
		rec := httptest.NewRecorder()
		srv.handleUpdateFeatures(rec, httptest.NewRequest(http.MethodPut, "/api/settings/features", bytes.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("update status=%d body=%q", rec.Code, rec.Body.String())
		}
	}
	put(45)
	if srv.config.SubscriptionHistoryRetentionDays != 45 {
		t.Fatalf("retention = %d; want 45", srv.config.SubscriptionHistoryRetentionDays)
	}
	put(-7)
	if srv.config.SubscriptionHistoryRetentionDays != 0 {
		t.Fatalf("a negative value must turn retention off, got %d", srv.config.SubscriptionHistoryRetentionDays)
	}

	rec := httptest.NewRecorder()
	srv.handleGetFeatures(rec, httptest.NewRequest(http.MethodGet, "/api/settings/features", nil))
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["subscription_history_retention_days"]; !ok || v.(float64) != 0 {
		t.Fatalf("features response = %v", got["subscription_history_retention_days"])
	}
}
