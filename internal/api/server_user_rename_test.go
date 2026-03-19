package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func findUserStatus(users []UserStatus, name string) *UserStatus {
	for i := range users {
		if users[i].Name == name {
			return &users[i]
		}
	}
	return nil
}

func inboundUserNames(t *testing.T, stub *singboxConfigExecutorStub, tag string) []string {
	t.Helper()

	inbound := readStoredInboundByTag(t, stub, tag)
	rawUsers, ok := inbound["users"].([]interface{})
	if !ok {
		t.Fatalf("inbound users missing or wrong type: %#v", inbound["users"])
	}

	names := make([]string, 0, len(rawUsers))
	for _, rawUser := range rawUsers {
		user, ok := rawUser.(map[string]interface{})
		if !ok {
			continue
		}
		if name, _ := user["name"].(string); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func TestHandleUpdateUser_RenamePreservesHistoricalTraffic(t *testing.T) {
	server, stub, store := newSingboxHandlerTestServerWithStore(t, `{
		"inbounds": [
			{
				"type":"vless",
				"tag":"test-vless",
				"listen":"0.0.0.0",
				"listen_port":443,
				"users":[
					{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111"}
				]
			}
		],
		"experimental":{
			"v2ray_api":{"listen":"127.0.0.1:19001","stats":{"enabled":true,"inbounds":["test-vless"],"outbounds":["direct"],"users":["alice"]}}
		}
	}`)

	if err := store.SaveUserMetadata(core.UserMetadata{
		Email:       "alice",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		ResetDay:    1,
		Enabled:     true,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
	}

	now := time.Now().Unix()
	if err := store.BulkInsert([]core.Sample{
		{User: "alice", Timestamp: now - 600, Uplink: 128, Downlink: 256},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	body, err := json.Marshal(CreateUserRequest{
		Name:         "alice-renamed",
		OriginalName: "alice",
		UUID:         "11111111-1111-1111-1111-111111111111",
		QuotaLimit:   0,
		QuotaPeriod:  "monthly",
		ResetDay:     1,
		Enabled:      boolPtr(true),
		InboundTag:   "test-vless",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/users", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleUpdateUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleUpdateUser status = %d; body = %s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	listRec := httptest.NewRecorder()
	server.handleGetUsers(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("handleGetUsers status = %d; body = %s", listRec.Code, listRec.Body.String())
	}

	var users []UserStatus
	if err := json.Unmarshal(listRec.Body.Bytes(), &users); err != nil {
		t.Fatalf("Unmarshal users: %v", err)
	}

	renamed := findUserStatus(users, "alice-renamed")
	if renamed == nil {
		t.Fatalf("renamed user not found in handleGetUsers response: %+v", users)
	}
	if renamed.Total != 384 {
		t.Fatalf("renamed user total = %d; want %d", renamed.Total, 384)
	}

	if stale := findUserStatus(users, "alice"); stale != nil {
		t.Fatalf("old user unexpectedly still visible after rename: %+v", *stale)
	}

	names := inboundUserNames(t, stub, "test-vless")
	if len(names) != 1 || names[0] != "alice-renamed" {
		t.Fatalf("stored inbound users = %#v; want [alice-renamed]", names)
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func TestHandleUpdateUser_RenameFailureRollsBackConfigIdentity(t *testing.T) {
	server, stub, store := newSingboxHandlerTestServerWithStore(t, `{
		"inbounds": [
			{
				"type":"vless",
				"tag":"test-vless",
				"listen":"0.0.0.0",
				"listen_port":443,
				"users":[
					{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111"}
				]
			}
		],
		"experimental":{
			"v2ray_api":{"listen":"127.0.0.1:19001","stats":{"enabled":true,"inbounds":["test-vless"],"outbounds":["direct"],"users":["alice"]}}
		}
	}`)

	if err := store.SaveUserMetadata(core.UserMetadata{
		Email:       "alice",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		ResetDay:    1,
		Enabled:     true,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata(old): %v", err)
	}
	if err := store.SaveUserMetadata(core.UserMetadata{
		Email:       "alice-renamed",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		ResetDay:    1,
		Enabled:     true,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata(conflict): %v", err)
	}
	if err := store.BulkInsert([]core.Sample{
		{User: "alice", Timestamp: time.Now().Unix() - 300, Uplink: 10, Downlink: 20},
	}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}

	body, err := json.Marshal(CreateUserRequest{
		Name:         "alice-renamed",
		OriginalName: "alice",
		UUID:         "11111111-1111-1111-1111-111111111111",
		QuotaLimit:   0,
		QuotaPeriod:  "monthly",
		ResetDay:     1,
		Enabled:      boolPtr(true),
		InboundTag:   "test-vless",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/users", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleUpdateUser(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("handleUpdateUser status = %d; want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "Failed to rename user traffic identity") {
		t.Fatalf("handleUpdateUser body = %q; want traffic-identity failure detail", rec.Body.String())
	}

	names := inboundUserNames(t, stub, "test-vless")
	if len(names) != 1 || names[0] != "alice" {
		t.Fatalf("stored inbound users = %#v; want rollback to [alice]", names)
	}
}
