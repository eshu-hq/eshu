// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestRepositoryDetailPayloadIndependentOfTerraformEvidence(t *testing.T) {
	asOf := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name           string
		queue          statuspkg.QueueSnapshot
		health         string
		nilCoordinator bool
	}{
		{"healthy", statuspkg.QueueSnapshot{Total: 5, Succeeded: 5}, "healthy", false},
		{"degraded", statuspkg.QueueSnapshot{Total: 6, Succeeded: 5, Failed: 1}, "degraded", false},
		{"stalled", statuspkg.QueueSnapshot{Total: 6, Succeeded: 5, Outstanding: 1, Pending: 1, OldestOutstandingAge: 15 * time.Minute}, "stalled", false},
		{"nil_coordinator", statuspkg.QueueSnapshot{Total: 5, Succeeded: 5}, "healthy", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := ingesterSelectionSnapshot(asOf)
			raw.Queue = tc.queue
			raw.ScopeActivity = statuspkg.ScopeActivitySnapshot{Active: 2, Changed: 1, Unchanged: 1}
			raw.StageCounts = []statuspkg.StageStatusCount{{Stage: "projector", Status: "pending", Count: 2}}
			for i := range 7 {
				outstanding := 0
				if tc.health != "healthy" {
					outstanding = 10 - i
				}
				raw.DomainBacklogs = append(raw.DomainBacklogs, statuspkg.DomainBacklog{Domain: fmt.Sprintf("domain-%d", i), Outstanding: outstanding})
			}
			if tc.nilCoordinator {
				raw.Coordinator = nil
			} else if tc.health == "healthy" {
				raw.Coordinator.ActiveClaims = 0
			}
			raw.TerraformStateLastSerials = []statuspkg.TerraformStateLocatorSerial{{SafeLocatorHash: "hash-a", BackendKind: "s3", Serial: 7, ObservedAt: asOf}}
			raw.TerraformStateRecentWarnings = []statuspkg.TerraformStateLocatorWarning{{SafeLocatorHash: "hash-a", WarningKind: "state_missing", ObservedAt: asOf}}
			for _, route := range []string{"/api/v0/status/ingesters/repository", "/api/v0/ingesters/repository"} {
				for _, scoped := range []bool{false, true} {
					name := fmt.Sprintf("%s/scoped=%t", route, scoped)
					t.Run(name, func(t *testing.T) {
						baseline := &selectionRecordingReader{snapshot: raw}
						selected := &selectionRecordingReader{snapshot: raw, omitTerraformOnSkip: true}
						readPayload := func(reader *selectionRecordingReader) map[string]any {
							t.Helper()
							h := &StatusHandler{StatusReader: reader}
							mux := http.NewServeMux()
							h.Mount(mux)
							req := httptest.NewRequest(http.MethodGet, route, nil)
							if scoped {
								req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{Mode: auth.AuthModeScoped}))
							}
							rec := httptest.NewRecorder()
							mux.ServeHTTP(rec, req)
							if rec.Code != http.StatusOK {
								t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
							}
							var payload map[string]any
							if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
								t.Fatal(err)
							}
							return payload
						}
						want := readPayload(baseline)
						got := readPayload(selected)
						if !baseline.lastSelection.SkipTerraformStateEvidence || !selected.lastSelection.SkipTerraformStateEvidence || baseline.filteredCallCount != 1 || selected.filteredCallCount != 1 {
							t.Fatalf("detail selection/calls = %+v/%d, %+v/%d", baseline.lastSelection, baseline.filteredCallCount, selected.lastSelection, selected.filteredCallCount)
						}
						if baseline.returnedTerraformRows != 2 || selected.returnedTerraformRows != 0 {
							t.Fatalf("Terraform rows baseline=%d selected=%d, want 2/0", baseline.returnedTerraformRows, selected.returnedTerraformRows)
						}
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("repository detail changed when Terraform evidence was omitted:\nfull=%#v\nomitted=%#v", want, got)
						}
						if got["health"].(map[string]any)["state"] != tc.health {
							t.Fatalf("health=%#v, want %s", got["health"], tc.health)
						}
						if got["queue"].(map[string]any)["total"] != float64(tc.queue.Total) || got["scope_activity"].(map[string]any)["active"] != float64(2) {
							t.Fatalf("queue/scope activity lost: %#v", got)
						}
						if len(got["stage_summaries"].([]any)) != 1 || len(got["domain_backlogs"].([]any)) != statuspkg.DefaultOptions().DomainLimit {
							t.Fatalf("stage/backlog bound lost: %#v", got)
						}
						coordinator := got["coordinator"].(map[string]any)
						if tc.nilCoordinator {
							if len(coordinator) != 0 {
								t.Fatalf("nil coordinator=%#v", coordinator)
							}
						} else if scoped {
							if coordinator["collector_instance_count"] != float64(2) {
								t.Fatalf("scoped coordinator=%#v", coordinator)
							}
						} else if len(coordinator["collector_instances"].([]any)) != 2 {
							t.Fatalf("coordinator=%#v", coordinator)
						}
					})
				}
			}
		})
	}
}
