package rediver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/califio/rediver-sdk-go/internal/api"
)

func TestDispatch_SmokeSyncsScannerMetadata(t *testing.T) {
	t.Parallel()

	type tokenRequest struct {
		Scanner    string `json:"scanner"`
		Persistent bool   `json:"persistent"`
	}

	var (
		mu           sync.Mutex
		gotTokenReq  tokenRequest
		gotUpdateReq api.UpdateAgentScannerRequest
		updateCalls  int
	)

	updateSeen := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/agent/generate-token":
			var req tokenRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode token request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			mu.Lock()
			gotTokenReq = req
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent_id": "agent-1",
				"token":    "agent-token-1",
			})
			return

		case r.Method == http.MethodPatch && r.URL.Path == "/api/agent/scanner":
			if got := r.Header.Get("X-Token"); got != "agent-token-1" {
				t.Errorf("X-Token = %q, want agent-token-1", got)
			}

			var req api.UpdateAgentScannerRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode update scanner request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			mu.Lock()
			gotUpdateReq = req
			updateCalls++
			mu.Unlock()

			select {
			case updateSeen <- struct{}{}:
			default:
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
			return

		case r.Method == http.MethodGet && r.URL.Path == "/api/agent/job/poll":
			w.WriteHeader(http.StatusNoContent)
			return

		case r.Method == http.MethodGet && r.URL.Path == "/api/agent/heartbeat":
			w.WriteHeader(http.StatusNoContent)
			return

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"audit_mode": map[string]interface{}{
				"type":  "string",
				"title": "Audit Mode",
			},
			"model": map[string]interface{}{
				"type": "string",
			},
		},
		"additionalProperties": false,
	}

	runner, err := NewRunner(server.URL, "cluster-token",
		WithDispatcherMetadataSync(),
		WithPollInterval(time.Second),
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	if err := runner.Add(NewScanner(
		"calif-audit",
		[]TargetType{TargetTypeRepository, TargetTypeService},
		nil,
		WithDisplayName("Calif Audit"),
		WithRawParamsSchema(schema),
	)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan error, 1)
	go func() {
		done <- runner.Dispatch(ctx, func(context.Context, PulledJob) error { return nil })
	}()

	select {
	case <-updateSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for scanner metadata sync")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Dispatch() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dispatcher shutdown")
	}

	mu.Lock()
	defer mu.Unlock()

	if gotTokenReq.Scanner != "calif-audit" {
		t.Fatalf("token scanner = %q, want calif-audit", gotTokenReq.Scanner)
	}
	if !gotTokenReq.Persistent {
		t.Fatal("expected dispatcher token request to be persistent")
	}
	if updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", updateCalls)
	}
	if gotUpdateReq.Name == nil || *gotUpdateReq.Name != "calif-audit" {
		t.Fatalf("update name = %v, want calif-audit", gotUpdateReq.Name)
	}
	if gotUpdateReq.DisplayName == nil || *gotUpdateReq.DisplayName != "Calif Audit" {
		t.Fatalf("display_name = %v, want Calif Audit", gotUpdateReq.DisplayName)
	}
	if gotUpdateReq.ParamsSchema == nil {
		t.Fatal("expected params_schema to be sent")
	}
	props, ok := (*gotUpdateReq.ParamsSchema)["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("params_schema.properties has unexpected type: %T", (*gotUpdateReq.ParamsSchema)["properties"])
	}
	if _, ok := props["audit_mode"]; !ok {
		t.Fatal("expected audit_mode in params_schema.properties")
	}
	if gotUpdateReq.AssetTypes == nil || len(*gotUpdateReq.AssetTypes) != 2 {
		t.Fatalf("asset_types = %v, want 2 entries", gotUpdateReq.AssetTypes)
	}
	if (*gotUpdateReq.AssetTypes)[0] != api.AssetTypesRepository {
		t.Fatalf("asset_types[0] = %q, want %q", (*gotUpdateReq.AssetTypes)[0], api.AssetTypesRepository)
	}
	if (*gotUpdateReq.AssetTypes)[1] != api.AssetTypesService {
		t.Fatalf("asset_types[1] = %q, want %q", (*gotUpdateReq.AssetTypes)[1], api.AssetTypesService)
	}
}
