// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// terraformSelectionAsOf is the fixed status clock for the #7009 Terraform
// route-selection proofs.
var terraformSelectionAsOf = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// terraformStatusRoute pairs one status route with the snapshot selection it
// must request.
type terraformStatusRoute struct {
	path string
	want statuspkg.SnapshotSelection
}

// fullWithoutTerraform is every optional status section except the
// Terraform-state serial and warning reads.
var fullWithoutTerraform = statuspkg.SnapshotSelection{
	IncludeCollectorFactEvidence: true,
	IncludeRegistryCollectors:    true,
	SkipTerraformStateEvidence:   true,
}

// ingesterWithoutTerraform keeps the ingester routes' existing omission of the
// fact_records aggregates and adds the Terraform-state omission.
var ingesterWithoutTerraform = statuspkg.SnapshotSelection{SkipTerraformStateEvidence: true}

// terraformFreeStatusRoutes lists every status route (and its legacy alias)
// whose response is built without Report.TerraformState. The MCP tools
// list_collectors, list_ingesters, get_ingester_status, get_hosted_readiness,
// get_operator_control_plane, get_freshness_causality,
// get_collector_readiness, get_hosted_governance_status, and
// get_answer_narration_status dispatch to these paths.
var terraformFreeStatusRoutes = []terraformStatusRoute{
	{"/api/v0/status/operations", fullWithoutTerraform},
	{"/api/v0/status/hosted-readiness", fullWithoutTerraform},
	{"/api/v0/status/operator-control-plane", fullWithoutTerraform},
	{"/api/v0/status/freshness-causality", fullWithoutTerraform},
	{"/api/v0/status/collectors", fullWithoutTerraform},
	{"/api/v0/collectors", fullWithoutTerraform},
	{"/api/v0/status/collector-readiness", fullWithoutTerraform},
	{"/api/v0/collector-readiness", fullWithoutTerraform},
	{"/api/v0/status/governance", fullWithoutTerraform},
	{"/api/v0/status/answer-narration", fullWithoutTerraform},
	{"/api/v0/status/ingesters", ingesterWithoutTerraform},
	{"/api/v0/ingesters", ingesterWithoutTerraform},
	{"/api/v0/status/ingesters/repository", ingesterWithoutTerraform},
	{"/api/v0/ingesters/repository", ingesterWithoutTerraform},
}

// terraformRenderingStatusRoutes lists the routes that render the
// terraform_state section and must keep reading its evidence.
var terraformRenderingStatusRoutes = []terraformStatusRoute{
	{"/api/v0/status/pipeline", statuspkg.FullSnapshotSelection()},
	{"/api/v0/status/index", statuspkg.SnapshotSelection{}},
	{"/api/v0/index-status", statuspkg.SnapshotSelection{}},
}

// terraformSelectionSnapshot is a populated snapshot that carries Terraform
// serials and warnings beside every other section the routes render.
func terraformSelectionSnapshot() statuspkg.RawSnapshot {
	raw := ingesterSelectionSnapshot(terraformSelectionAsOf)
	raw.Queue = statuspkg.QueueSnapshot{Total: 6, Succeeded: 5, Outstanding: 1, Pending: 1}
	raw.ScopeActivity = statuspkg.ScopeActivitySnapshot{Active: 2, Changed: 1, Unchanged: 1}
	raw.StageCounts = []statuspkg.StageStatusCount{{Stage: "projector", Status: "pending", Count: 1}}
	raw.DomainBacklogs = []statuspkg.DomainBacklog{{Domain: "code_calls", Outstanding: 1}}
	raw.TerraformStateLastSerials = []statuspkg.TerraformStateLocatorSerial{
		{SafeLocatorHash: "hash-a", BackendKind: "s3", Serial: 7, ObservedAt: terraformSelectionAsOf},
	}
	raw.TerraformStateRecentWarnings = []statuspkg.TerraformStateLocatorWarning{
		{SafeLocatorHash: "hash-a", BackendKind: "s3", WarningKind: "state_missing", ObservedAt: terraformSelectionAsOf},
		{SafeLocatorHash: "repo-1:infra/main.tf", BackendKind: "git", WarningKind: "unresolved_backend_expression", ObservedAt: terraformSelectionAsOf},
	}
	return raw
}

// serveStatusRoute mounts a fresh StatusHandler over reader and returns the
// response for one GET of path.
func serveStatusRoute(t *testing.T, reader statuspkg.Reader, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := &StatusHandler{StatusReader: reader, LiveActivity: &fakeLiveActivityReader{}}
	mux := http.NewServeMux()
	h.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, body=%s", path, rec.Code, rec.Body.String())
	}
	return rec
}

// TestTerraformFreeStatusRoutesSkipTerraformEvidence proves each route that
// never renders Report.TerraformState requests a selection without the
// Terraform-state reads, and that its response bytes are identical whether the
// reader returns Terraform evidence or omits it.
func TestTerraformFreeStatusRoutesSkipTerraformEvidence(t *testing.T) {
	t.Parallel()
	for _, route := range terraformFreeStatusRoutes {
		t.Run(route.path, func(t *testing.T) {
			t.Parallel()
			baseline := &selectionRecordingReader{snapshot: terraformSelectionSnapshot()}
			selected := &selectionRecordingReader{snapshot: terraformSelectionSnapshot(), omitTerraformOnSkip: true}
			want := serveStatusRoute(t, baseline, route.path).Body.Bytes()
			got := serveStatusRoute(t, selected, route.path).Body.Bytes()
			if selected.filteredCallCount != 1 || selected.lastSelection != route.want {
				t.Fatalf("selection = %+v (calls %d), want %+v (calls 1)", selected.lastSelection, selected.filteredCallCount, route.want)
			}
			if baseline.returnedTerraformRows != 3 || selected.returnedTerraformRows != 0 {
				t.Fatalf("Terraform rows baseline=%d selected=%d, want 3/0", baseline.returnedTerraformRows, selected.returnedTerraformRows)
			}
			if !statusBodiesEqual(t, route.path, got, want) {
				t.Fatalf("response changed when Terraform evidence was omitted:\nfull=%s\nomitted=%s", want, got)
			}
		})
	}
}

// TestTerraformRenderingStatusRoutesKeepTerraformEvidence proves the routes
// that render terraform_state still read its evidence, and that the
// byte-equality probe above detects a route that would lose that section
// (seeded violation: the reader omits Terraform rows regardless of selection).
func TestTerraformRenderingStatusRoutesKeepTerraformEvidence(t *testing.T) {
	t.Parallel()
	for _, route := range terraformRenderingStatusRoutes {
		t.Run(route.path, func(t *testing.T) {
			t.Parallel()
			full := &selectionRecordingReader{snapshot: terraformSelectionSnapshot(), omitTerraformOnSkip: true}
			body := serveStatusRoute(t, full, route.path).Body.Bytes()
			if full.filteredCallCount != 1 || full.lastSelection != route.want {
				t.Fatalf("selection = %+v (calls %d), want %+v (calls 1)", full.lastSelection, full.filteredCallCount, route.want)
			}
			if full.returnedTerraformRows != 3 {
				t.Fatalf("Terraform rows = %d, want 3", full.returnedTerraformRows)
			}
			for _, marker := range []string{`"terraform_state"`, `"hash-a"`, `"state_missing"`, `"unresolved_backend_expression"`} {
				if !strings.Contains(string(body), marker) {
					t.Fatalf("response lost %s: %s", marker, body)
				}
			}
			omitted := terraformSelectionSnapshot()
			omitted.TerraformStateLastSerials = nil
			omitted.TerraformStateRecentWarnings = nil
			violation := serveStatusRoute(t, &selectionRecordingReader{snapshot: omitted}, route.path).Body.Bytes()
			if bytes.Equal(body, violation) {
				t.Fatalf("byte-equality probe cannot see a lost terraform_state section on %s", route.path)
			}
		})
	}
}

// handlerClockFields names the top-level response fields a route stamps from
// its own time.Now() rather than from the snapshot clock. Collector readiness
// writes generated_at at second precision, so two renders can straddle a
// second boundary; the probe drops only these fields before comparing.
var handlerClockFields = map[string][]string{
	"/api/v0/status/collector-readiness": {"generated_at"},
	"/api/v0/collector-readiness":        {"generated_at"},
}

// statusBodiesEqual reports whether two bodies of one route are identical,
// ignoring only that route's handler-clock fields. Each ignored field must be
// present in both bodies, so a renamed field cannot make the probe vacuous.
func statusBodiesEqual(t *testing.T, path string, a, b []byte) bool {
	t.Helper()
	fields := handlerClockFields[path]
	if len(fields) == 0 {
		return bytes.Equal(a, b)
	}
	normalize := func(body []byte) []byte {
		t.Helper()
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("%s: decode body: %v: %s", path, err, body)
		}
		for _, field := range fields {
			if _, ok := payload[field]; !ok {
				t.Fatalf("%s: body has no handler-clock field %q: %s", path, field, body)
			}
			delete(payload, field)
		}
		out, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("%s: encode body: %v", path, err)
		}
		return out
	}
	return bytes.Equal(normalize(a), normalize(b))
}

// TestStatusBodiesEqualIgnoresOnlyHandlerClock proves the equality probe
// tolerates a second-boundary difference in the readiness handler's own
// wall-clock stamp and nothing else.
func TestStatusBodiesEqualIgnoresOnlyHandlerClock(t *testing.T) {
	t.Parallel()
	first := []byte(`{"count":1,"generated_at":"2026-10-06T08:27:37Z","readiness":[]}`)
	nextSecond := []byte(`{"count":1,"generated_at":"2026-10-06T08:27:38Z","readiness":[]}`)
	otherCount := []byte(`{"count":2,"generated_at":"2026-10-06T08:27:37Z","readiness":[]}`)
	for _, path := range []string{"/api/v0/status/collector-readiness", "/api/v0/collector-readiness"} {
		if !statusBodiesEqual(t, path, first, nextSecond) {
			t.Fatalf("%s: a generated_at second boundary failed the probe", path)
		}
		if statusBodiesEqual(t, path, first, otherCount) {
			t.Fatalf("%s: a changed count passed the probe", path)
		}
	}
	if statusBodiesEqual(t, "/api/v0/status/operations", first, nextSecond) {
		t.Fatal("a route without a handler clock ignored a changed field")
	}
}
