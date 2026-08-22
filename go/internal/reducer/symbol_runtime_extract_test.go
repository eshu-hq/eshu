// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// symbolRuntimeFileEnvelope builds one file fact envelope carrying functions,
// a single-framework route_entries slice, and function_calls together, so a
// single Function entity can drive all three symbol->runtime domains
// (handles_route/runs_in read route_entries; invokes_cloud_action reads
// function_calls) from the same parsed_file_data the production parser emits.
// framework == "" omits framework_semantics entirely (no route data).
func symbolRuntimeFileEnvelope(
	repoID string,
	relativePath string,
	functions []map[string]any,
	framework string,
	routeEntries []any,
	calls []map[string]any,
) facts.Envelope {
	callSlice := make([]any, 0, len(calls))
	for _, call := range calls {
		callSlice = append(callSlice, call)
	}
	parsedFileData := map[string]any{
		"path":           relativePath,
		"functions":      functions,
		"function_calls": callSlice,
	}
	if framework != "" {
		parsedFileData["framework_semantics"] = map[string]any{
			"frameworks": []any{framework},
			framework: map[string]any{
				"route_entries": routeEntries,
			},
		}
	}
	return facts.Envelope{
		FactKind: "file",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":          repoID,
			"relative_path":    relativePath,
			"parsed_file_data": parsedFileData,
		},
	}
}

// filterByDomain returns only the rows for one ProjectionDomain.
func filterByDomain(rows []SharedProjectionIntentRow, domain string) []SharedProjectionIntentRow {
	out := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if row.ProjectionDomain == domain {
			out = append(out, row)
		}
	}
	return out
}

// symbolRuntimeIsRefreshRow reports whether a row is the paired per-repo
// repo-wide refresh intent (action: "refresh", intent_type: "repo_refresh"),
// as opposed to a per-edge upsert row. A consumer that wants edges must
// filter these out, mirroring the production filterUpsertRows gate.
func symbolRuntimeIsRefreshRow(row SharedProjectionIntentRow) bool {
	return payloadStr(row.Payload, "action") == "refresh"
}

// filterEdgeRows keeps only the per-edge (non-refresh) rows for a domain.
func filterEdgeRows(rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	out := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if !symbolRuntimeIsRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

func TestExtractSymbolRuntimeIntentRowsEmptyEnvelopes(t *testing.T) {
	t.Parallel()

	rows := ExtractSymbolRuntimeIntentRows(nil, "gen-1", time.Unix(0, 0).UTC())
	if len(rows) != 0 {
		t.Fatalf("expected no rows for empty envelopes, got %d: %+v", len(rows), rows)
	}
}

func TestExtractSymbolRuntimeIntentRowsHandlesRouteHappyPath(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		symbolRuntimeFileEnvelope(
			"repo-1",
			"server.go",
			[]map[string]any{
				{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 1, "end_line": 100},
			},
			"net_http",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "getWidgets"},
			},
			nil,
		),
	}

	rows := ExtractSymbolRuntimeIntentRows(envelopes, "gen-1", time.Unix(0, 0).UTC())
	edgeRows := filterEdgeRows(filterByDomain(rows, DomainHandlesRoute))
	if len(edgeRows) != 1 {
		t.Fatalf("expected exactly 1 HANDLES_ROUTE upsert row, got %d: %+v", len(edgeRows), edgeRows)
	}

	row := edgeRows[0]
	// Assert every payload field canonical_handles_route_edges.go's row-map
	// builder reads: function_entity_id/repo_id/path feed the two MATCH
	// clauses' identity, the rest are SET-only relationship properties.
	if got, want := payloadStr(row.Payload, "function_entity_id"), "content-entity:gw"; got != want {
		t.Fatalf("function_entity_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(row.Payload, "repo_id"), "repo-1"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(row.Payload, "path"), "/widgets"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := payloadStr(row.Payload, "http_method"), "GET"; got != want {
		t.Fatalf("http_method = %q, want %q", got, want)
	}
	if got, want := payloadStr(row.Payload, "framework"), "net_http"; got != want {
		t.Fatalf("framework = %q, want %q", got, want)
	}
	if got, want := payloadStr(row.Payload, "evidence_source"), handlesRouteEvidenceSource; got != want {
		t.Fatalf("evidence_source = %q, want %q", got, want)
	}
	if payloadStr(row.Payload, "resolution_method") == "" {
		t.Fatalf("resolution_method is empty, want a classified provenance method")
	}
	if _, ok := row.Payload["confidence"].(float64); !ok {
		t.Fatalf("confidence missing or not a float64: %+v", row.Payload["confidence"])
	}
	if payloadStr(row.Payload, "reason") == "" {
		t.Fatalf("reason is empty")
	}
}

// TestExtractSymbolRuntimeIntentRowsHandlesRouteMethodDedupeSharedPartitionKey
// is a required regression test: two route entries on the same path/handler
// but different HTTP methods (GET, POST) must produce TWO distinct
// SharedProjectionIntentRows -- the intent-level dedupe key includes
// http_method (handles_route_intents.go:80) -- but both rows share the SAME
// PartitionKey, because the partition key omits the method
// (handles_route_intents.go:101).
//
// This matters because the graph-write MERGE identity
// (canonical_handles_route_edges.go:16-24) is only the
// (Function, HANDLES_ROUTE, Endpoint) node pair -- no relationship property,
// including http_method, participates in the MERGE. So both rows MERGE the
// same relationship instance at write time and each SETs rel.http_method to
// its own value: the two intent rows collapse to exactly ONE graph edge,
// with whichever row's write lands last winning the stored http_method.
func TestExtractSymbolRuntimeIntentRowsHandlesRouteMethodDedupeSharedPartitionKey(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		symbolRuntimeFileEnvelope(
			"repo-1",
			"server.go",
			[]map[string]any{
				{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 1, "end_line": 100},
			},
			"net_http",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "getWidgets"},
				map[string]any{"method": "POST", "path": "/widgets", "handler": "getWidgets"},
			},
			nil,
		),
	}

	rows := ExtractSymbolRuntimeIntentRows(envelopes, "gen-1", time.Unix(0, 0).UTC())
	edgeRows := filterEdgeRows(filterByDomain(rows, DomainHandlesRoute))
	if len(edgeRows) != 2 {
		t.Fatalf("expected exactly 2 HANDLES_ROUTE upsert rows (one per method), got %d: %+v", len(edgeRows), edgeRows)
	}

	methods := make(map[string]bool)
	partitionKeys := make(map[string]struct{})
	for _, row := range edgeRows {
		methods[payloadStr(row.Payload, "http_method")] = true
		partitionKeys[row.PartitionKey] = struct{}{}
	}
	if !methods["GET"] || !methods["POST"] {
		t.Fatalf("expected one GET row and one POST row, got methods=%v", methods)
	}
	if len(partitionKeys) != 1 {
		t.Fatalf("expected GET and POST rows to share ONE partition key (method is not part of the "+
			"partition key), got %d distinct keys: %v", len(partitionKeys), partitionKeys)
	}
}

func TestExtractSymbolRuntimeIntentRowsRunsIn(t *testing.T) {
	t.Parallel()

	// runs_in reads the SAME handlesRouteEntries the code stage reads for
	// handles_route (runs_in_intents.go:85), so the same route_entries data
	// that drives HANDLES_ROUTE also drives RUNS_IN for the resolved handler
	// Function. Two route entries collapse to one RUNS_IN row because its
	// dedupe key is (functionID, repositoryID) only -- it has no method
	// dimension.
	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		symbolRuntimeFileEnvelope(
			"repo-1",
			"server.go",
			[]map[string]any{
				{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 1, "end_line": 100},
			},
			"net_http",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "getWidgets"},
				map[string]any{"method": "POST", "path": "/widgets", "handler": "getWidgets"},
			},
			nil,
		),
	}

	rows := ExtractSymbolRuntimeIntentRows(envelopes, "gen-1", time.Unix(0, 0).UTC())
	edgeRows := filterEdgeRows(filterByDomain(rows, DomainRunsIn))
	if len(edgeRows) != 1 {
		t.Fatalf("expected exactly 1 RUNS_IN upsert row (dedupe collapses both route entries), got %d: %+v",
			len(edgeRows), edgeRows)
	}

	row := edgeRows[0]
	if got, want := payloadStr(row.Payload, "function_id"), "content-entity:gw"; got != want {
		t.Fatalf("function_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(row.Payload, "repo_id"), "repo-1"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
	// The code stage never proves a repo defines exactly one Workload, so every
	// RUNS_IN edge is conservatively a candidate binding: ambiguous=true.
	if ambiguous, ok := row.Payload["ambiguous"].(bool); !ok || !ambiguous {
		t.Fatalf("ambiguous = %v (ok=%v), want true", row.Payload["ambiguous"], ok)
	}
	// Note (not asserted numerically here): at write time, one RUNS_IN intent
	// row fans out to N graph edges for N Workloads the repo DEFINES
	// (canonical_runs_in_edges.go:24-31, no LIMIT) -- this seam's row count is
	// not the eventual edge count.
}

func TestExtractSymbolRuntimeIntentRowsInvokesCloudAction(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		symbolRuntimeFileEnvelope(
			"repo-1",
			"main.go",
			[]map[string]any{
				{"name": "Handler", "uid": "content-entity:handler", "line_number": 1, "end_line": 100},
			},
			"",
			nil,
			[]map[string]any{
				// In the closed cloudActionByServiceMethod table: must emit.
				{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 50},
				// NOT in the closed table: must emit nothing.
				{"name": "Frobnicate", "receiver_sdk_service": "widget", "line_number": 60},
			},
		),
	}

	rows := ExtractSymbolRuntimeIntentRows(envelopes, "gen-1", time.Unix(0, 0).UTC())
	edgeRows := filterEdgeRows(filterByDomain(rows, DomainInvokesCloudAction))
	if len(edgeRows) != 1 {
		t.Fatalf("expected exactly 1 INVOKES_CLOUD_ACTION upsert row (non-catalog call must emit nothing), "+
			"got %d: %+v", len(edgeRows), edgeRows)
	}

	row := edgeRows[0]
	if got, want := payloadStr(row.Payload, "function_id"), "content-entity:handler"; got != want {
		t.Fatalf("function_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(row.Payload, "repo_id"), "repo-1"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
	// REQUIRED: the resolved action lives under "cloud_action", NEVER "action".
	// filterUpsertRows treats payload["action"] as the upsert/refresh/delete
	// discriminator (shared_projection_readiness.go:245-258); if the cloud
	// action string were stored under "action" every row would look like a
	// non-"upsert" control row and silently drop every edge.
	if got, want := payloadStr(row.Payload, "cloud_action"), "s3:putobject"; got != want {
		t.Fatalf("cloud_action = %q, want %q", got, want)
	}
	if _, hasAction := row.Payload["action"]; hasAction {
		t.Fatalf("payload MUST NOT carry an \"action\" key for a per-edge row, got %v", row.Payload["action"])
	}
	if got, want := payloadStr(row.Payload, "action_id"), "cloud-action:s3:putobject"; got != want {
		t.Fatalf("action_id = %q, want %q", got, want)
	}
	if got, want := payloadStr(row.Payload, "evidence_source"), invokesCloudActionEvidenceSource; got != want {
		t.Fatalf("evidence_source = %q, want %q", got, want)
	}
}

// TestExtractSymbolRuntimeIntentRowsRepoWideRefreshPairing asserts the
// refresh-fence pairing invariant every per-edge row for a repo-wide-retract
// domain must carry: exactly one action:"refresh" row per repo per domain
// with per-edge rows, and every per-edge row for that domain marked
// retract_via_refresh=true. A seam that dropped this pairing would look
// correct in isolation but wedge the worker's refresh fence, because
// per-edge rows would defer forever waiting for a refresh that never comes
// (or bypass the fence unexpectedly, retracting via the legacy per-partition
// path instead).
func TestExtractSymbolRuntimeIntentRowsRepoWideRefreshPairing(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		handlesRouteRepoEnvelope("repo-1"),
		symbolRuntimeFileEnvelope(
			"repo-1",
			"server.go",
			[]map[string]any{
				{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 1, "end_line": 100},
			},
			"net_http",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "getWidgets"},
			},
			[]map[string]any{
				{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 50},
			},
		),
	}

	rows := ExtractSymbolRuntimeIntentRows(envelopes, "gen-1", time.Unix(0, 0).UTC())

	for _, domain := range []string{DomainHandlesRoute, DomainRunsIn, DomainInvokesCloudAction} {
		domainRows := filterByDomain(rows, domain)
		var refreshCount int
		var edgeCount int
		for _, row := range domainRows {
			if symbolRuntimeIsRefreshRow(row) {
				refreshCount++
				if payloadStr(row.Payload, "intent_type") != RepoRefreshIntentType {
					t.Fatalf("[%s] refresh row intent_type = %q, want %q",
						domain, payloadStr(row.Payload, "intent_type"), RepoRefreshIntentType)
				}
				continue
			}
			edgeCount++
			if !payloadBool(row.Payload, "retract_via_refresh") {
				t.Fatalf("[%s] per-edge row missing retract_via_refresh=true: %+v", domain, row.Payload)
			}
		}
		if edgeCount == 0 {
			t.Fatalf("[%s] expected at least one per-edge row in this fixture, got 0", domain)
		}
		if refreshCount != 1 {
			t.Fatalf("[%s] expected exactly 1 repo-wide refresh row for repo-1, got %d", domain, refreshCount)
		}
	}
}

// TestExtractSymbolRuntimeIntentRowsDeterministic asserts calling the seam
// twice on the same input with the same createdAt returns identical rows in
// identical order -- required because these rows feed an ordering-safe
// shared-projection write path.
func TestExtractSymbolRuntimeIntentRowsDeterministic(t *testing.T) {
	t.Parallel()

	buildEnvelopes := func() []facts.Envelope {
		return []facts.Envelope{
			handlesRouteRepoEnvelope("repo-1"),
			symbolRuntimeFileEnvelope(
				"repo-1",
				"server.go",
				[]map[string]any{
					{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 1, "end_line": 100},
				},
				"net_http",
				[]any{
					map[string]any{"method": "GET", "path": "/widgets", "handler": "getWidgets"},
					map[string]any{"method": "POST", "path": "/widgets", "handler": "getWidgets"},
				},
				[]map[string]any{
					{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 50},
					{"name": "Frobnicate", "receiver_sdk_service": "widget", "line_number": 60},
				},
			),
		}
	}

	createdAt := time.Unix(0, 0).UTC()
	first := ExtractSymbolRuntimeIntentRows(buildEnvelopes(), "gen-1", createdAt)
	second := ExtractSymbolRuntimeIntentRows(buildEnvelopes(), "gen-1", createdAt)

	if len(first) == 0 {
		t.Fatalf("expected at least one row from the fixture, got 0")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("ExtractSymbolRuntimeIntentRows is not deterministic:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

// TestExtractSymbolRuntimeIntentRowsEmptySourceRunIDYieldsZeroRows is a
// regression guard for a silent-zero trap: buildCodeCallProjectionContexts
// skips building a ProjectionContext entry for a repository fact whose
// source_run_id is empty (code_call_materialization_intents.go:63-65). All
// three builders (buildHandlesRouteIntentRows, buildRunsInIntentRows,
// buildInvokesCloudActionIntentRows) early-return nil the moment
// contextByRepoID has no entry for a file's repo_id, so a repository fact
// missing source_run_id produces ZERO rows for every domain -- not an error,
// not a partial result, just silence. A hand-built odu/cassette that omits
// source_run_id would look like it wired everything correctly and still
// prove nothing on a live gate.
func TestExtractSymbolRuntimeIntentRowsEmptySourceRunIDYieldsZeroRows(t *testing.T) {
	t.Parallel()

	repoNoSourceRun := facts.Envelope{
		FactKind: "repository",
		ScopeID:  "scope-1",
		Payload: map[string]any{
			"repo_id":       "repo-1",
			"source_run_id": "",
		},
	}
	envelopes := []facts.Envelope{
		repoNoSourceRun,
		symbolRuntimeFileEnvelope(
			"repo-1",
			"server.go",
			[]map[string]any{
				{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 1, "end_line": 100},
			},
			"net_http",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "getWidgets"},
			},
			[]map[string]any{
				{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 50},
			},
		),
	}

	rows := ExtractSymbolRuntimeIntentRows(envelopes, "gen-1", time.Unix(0, 0).UTC())
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for a repository fact with an empty source_run_id "+
			"(no ProjectionContext is ever built for it), got %d: %+v", len(rows), rows)
	}

	for _, domain := range []string{DomainHandlesRoute, DomainRunsIn, DomainInvokesCloudAction} {
		if got := filterByDomain(rows, domain); len(got) != 0 {
			t.Fatalf("[%s] expected 0 rows with empty source_run_id, got %d", domain, len(got))
		}
	}
}
