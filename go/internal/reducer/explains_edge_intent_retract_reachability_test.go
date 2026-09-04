// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/rationale"
)

// rejectingFenceLookup fails the test if the refresh fence is consulted at all.
// Every per-edge row these tests build belongs to a repository whose refresh
// row is in the same batch, so planRepoWideRetractWork takes the
// same-cycle-ordering branch and must never reach the durable fence.
type rejectingFenceLookup struct{ t *testing.T }

func (f rejectingFenceLookup) HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents(
	_ context.Context,
	_ SharedProjectionAcceptanceKey,
	_ string,
	_ string,
	_ string,
) (bool, error) {
	f.t.Helper()
	f.t.Fatal("refresh fence consulted for a repository whose refresh row is in the same batch")
	return false, nil
}

// roundTripPayloads marshals every row's payload to JSON and back, the way the
// durable shared-intent store persists and reloads it. The retract_via_refresh
// marker is only load-bearing if it survives that trip: a marker that decodes
// to a shape rowUsesRefreshFence does not recognize would make an ordinary
// per-edge row look "unmarked legacy" to planRepoWideRetractWork.
func roundTripPayloads(t *testing.T, rows []SharedProjectionIntentRow) []SharedProjectionIntentRow {
	t.Helper()
	out := make([]SharedProjectionIntentRow, len(rows))
	copy(out, rows)
	for i := range out {
		if out[i].Payload == nil {
			continue
		}
		encoded, err := json.Marshal(out[i].Payload)
		if err != nil {
			t.Fatalf("marshal payload for %s: %v", out[i].IntentID, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal payload for %s: %v", out[i].IntentID, err)
		}
		out[i].Payload = decoded
	}
	return out
}

// TestRationaleProductionIntentsNeverReachRetractAsUnmarkedRows is the
// reachability half of #6166.
//
// collectRepoIDs (storage/cypher/edge_writer_retract_scope.go) binds the WHOLE
// batch into the non-delta whole-repository EXPLAINS DELETE with no
// intent_type filter, so any rationale row that reaches RetractEdges as a
// retract row hands its repository a repo-wide delete. That is only a live
// over-delete if a rationale row WITHOUT the refresh intent_type can actually
// reach retractRows.
//
// It cannot, and this pins why: buildRationaleSharedIntentRows stamps
// retract_via_refresh on every per-edge intent it emits
// (rationale_edge_intents.go), and planRepoWideRetractWork routes a marked
// per-edge row to writeRows, never to retractRows. Only the refresh rows --
// which all carry intent_type -- retract. If a future change stops stamping the
// marker, or the marker stops surviving the durable round trip, this test goes
// red and the over-delete becomes live.
func TestRationaleProductionIntentsNeverReachRetractAsUnmarkedRows(t *testing.T) {
	t.Parallel()

	const repoA, repoB = "repo-a", "repo-b"
	contextByRepoID := map[string]ProjectionContext{
		repoA: {ScopeID: "scope-a", SourceRunID: "run-1", GenerationID: "gen-1"},
		repoB: {ScopeID: "scope-b", SourceRunID: "run-1", GenerationID: "gen-1"},
	}
	edges := []map[string]any{
		{"repo_id": repoA, "target_path": "src/a.go", "rationale_uid": "r:a", "target_entity_id": "e:a"},
		{"repo_id": repoB, "target_path": "src/b.go", "rationale_uid": "r:b", "target_entity_id": "e:b"},
	}
	rows := rationale.BuildSharedIntentRows(
		edges, rationale.DeltaScope{}, []string{repoA, repoB}, contextByRepoID,
		time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
	)
	if len(rows) != 4 {
		t.Fatalf("emitted intents = %d, want 4 (2 refresh + 2 per-edge)", len(rows))
	}

	plan, err := planRepoWideRetractWork(
		context.Background(),
		DomainRationaleEdges,
		roundTripPayloads(t, rows),
		rejectingFenceLookup{t: t},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("planRepoWideRetractWork: %v", err)
	}

	// Positive half: the refresh rows must still retract. An assertion that
	// only forbade rows would pass vacuously if the whole plan went empty.
	if len(plan.retractRows) != 2 {
		t.Fatalf("retractRows = %d, want 2 (one refresh per repository)", len(plan.retractRows))
	}
	if len(plan.writeRows) != 2 {
		t.Fatalf("writeRows = %d, want 2 (both per-edge rows write this cycle)", len(plan.writeRows))
	}

	// Negative half: nothing reaching the retract may lack the intent_type that
	// collectRepoIDs does not check for.
	for _, row := range plan.retractRows {
		if payloadStr(row.Payload, "intent_type") != RepoRefreshIntentType {
			t.Fatalf("intent %s reached retractRows without intent_type=%q (payload %#v); "+
				"collectRepoIDs binds it into a whole-repository EXPLAINS DELETE",
				row.IntentID, RepoRefreshIntentType, row.Payload)
		}
	}
}

// TestRationalePerEdgeIntentsCarryRefreshFenceMarkerAfterRoundTrip is the
// narrower pin under the reachability test above: the marker itself, read back
// exactly as the worker reads it, after the durable JSON round trip.
func TestRationalePerEdgeIntentsCarryRefreshFenceMarkerAfterRoundTrip(t *testing.T) {
	t.Parallel()

	const repoID = "repo-marker"
	rows := rationale.BuildSharedIntentRows(
		[]map[string]any{
			{"repo_id": repoID, "target_path": "src/x.go", "rationale_uid": "r:x", "target_entity_id": "e:x"},
		},
		rationale.DeltaScope{},
		[]string{repoID},
		map[string]ProjectionContext{repoID: {ScopeID: "scope-x", SourceRunID: "run-1", GenerationID: "gen-1"}},
		time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
	)

	perEdge := 0
	for _, row := range roundTripPayloads(t, rows) {
		if isRepoRefreshRow(row) {
			continue
		}
		perEdge++
		if !rowUsesRefreshFence(row) {
			t.Fatalf("per-edge intent %s lost its %s marker across the durable round trip (payload %#v)",
				row.IntentID, retractViaRefreshKey, row.Payload)
		}
	}
	if perEdge != 1 {
		t.Fatalf("per-edge intents = %d, want 1", perEdge)
	}
}
