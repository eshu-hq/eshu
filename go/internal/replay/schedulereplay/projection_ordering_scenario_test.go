// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedulereplay_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// projectionCase is one shared-conflict-key reducer projection under proof:
// a reducer_domain written by >=2 distinct projection hooks in
// specs/fact-kind-registry.v1.yaml (C-14 #4367). incident_repository_correlation
// is written by incident_context_read_model and work_item_evidence_read_model;
// supply_chain_impact is written by supply_chain_impact, vulnerability_source_state,
// and vulnerability_suppression_admission.
type projectionCase struct {
	name         string
	cassettePath string
	domain       reducer.Domain
}

var projectionCases = []projectionCase{
	{
		name:         "incident_repository_correlation",
		cassettePath: filepath.Join("..", "..", "..", "..", "testdata", "cassettes", "replayschedule", "incident-repository-correlation.json"),
		domain:       reducer.DomainIncidentRepositoryCorrelation,
	},
	{
		name:         "supply_chain_impact",
		cassettePath: filepath.Join("..", "..", "..", "..", "testdata", "cassettes", "replayschedule", "supply-chain-impact.json"),
		domain:       reducer.DomainSupplyChainImpact,
	},
}

// loadProjectionItems loads a projection cassette and asserts the shared-
// conflict-key guard: the resulting work items must carry at least 4 items
// spanning at least 2 distinct projection-hook IntentID prefixes. Fewer than 2
// hooks would mean the cassette collapsed to a single-hook scenario and could
// no longer prove cross-hook ordering, silently defeating the point of this
// test file.
func loadProjectionItems(t *testing.T, path string) []schedulereplay.WorkItem {
	t.Helper()
	items, err := schedulereplay.LoadProjectionWorkItems(path)
	if err != nil {
		t.Fatalf("LoadProjectionWorkItems(%s): %v", path, err)
	}
	if len(items) < 4 {
		t.Fatalf("want at least 4 work items, got %d", len(items))
	}
	hooks := map[string]struct{}{}
	for _, item := range items {
		hook, _, ok := strings.Cut(item.IntentID, ":")
		if !ok || hook == "" {
			t.Fatalf("work item IntentID %q has no projection-hook prefix", item.IntentID)
		}
		hooks[hook] = struct{}{}
	}
	if len(hooks) < 2 {
		t.Fatalf("want >=2 distinct projection-hook prefixes (shared-conflict-key guard), got %d: %v", len(hooks), hooks)
	}
	return items
}

// TestProjectionScheduleReplayOrderInvariantSnapshot is the C-14 (#4367)
// acceptance for both shared-conflict-key projections: for each domain, the
// same recorded work items delivered through the deterministic in-memory
// source in four scripted orders (in-order, reverse, rotated, with-duplicates),
// drained through the real reducer service loop under that projection's real
// domain constant, converge on one byte-identical canonical graph snapshot.
func TestProjectionScheduleReplayOrderInvariantSnapshot(t *testing.T) {
	t.Parallel()

	for _, tc := range projectionCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items := loadProjectionItems(t, tc.cassettePath)

			orders := map[string][]schedulereplay.WorkItem{
				"in-order":        schedulereplay.ScheduleInOrder(items),
				"reverse":         schedulereplay.ScheduleReverse(items),
				"rotated":         schedulereplay.ScheduleRotated(items, 2),
				"with-duplicates": schedulereplay.ScheduleWithDuplicates(items),
			}

			var baselineName string
			var baseline []byte
			for name, scheduleItems := range orders {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				snap, err := schedulereplay.RunSchedule(ctx, schedulereplay.Config{
					Items:   scheduleItems,
					Workers: 1,
					Apply:   schedulereplay.ApplyCanonical,
					Domain:  tc.domain,
				})
				cancel()
				if err != nil {
					t.Fatalf("RunSchedule(%s): %v", name, err)
				}
				if len(snap) == 0 {
					t.Fatalf("RunSchedule(%s): empty snapshot", name)
				}
				if baseline == nil {
					baselineName, baseline = name, snap
					continue
				}
				if !bytes.Equal(baseline, snap) {
					t.Fatalf("snapshot for order %q differs from order %q:\n%s\n---\n%s",
						name, baselineName, baseline, snap)
				}
			}
		})
	}
}

// TestProjectionScheduleReplayConcurrentBatchInvariant exercises the real
// concurrent BatchWorkSource.ClaimBatch path (4 workers) for each
// shared-conflict-key projection domain and asserts the converged snapshot
// still equals the deterministic sequential one — genuine concurrency on the
// shared reducer_domain conflict key must not change the converged graph
// truth.
func TestProjectionScheduleReplayConcurrentBatchInvariant(t *testing.T) {
	t.Parallel()

	for _, tc := range projectionCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items := loadProjectionItems(t, tc.cassettePath)

			seqCtx, seqCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer seqCancel()
			seqSnap, err := schedulereplay.RunSchedule(seqCtx, schedulereplay.Config{
				Items:   schedulereplay.ScheduleInOrder(items),
				Workers: 1,
				Apply:   schedulereplay.ApplyCanonical,
				Domain:  tc.domain,
			})
			if err != nil {
				t.Fatalf("sequential RunSchedule: %v", err)
			}

			concCtx, concCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer concCancel()
			concSnap, batchCalls, err := schedulereplay.RunScheduleReport(concCtx, schedulereplay.Config{
				Items:   schedulereplay.ScheduleReverse(items),
				Workers: 4,
				Apply:   schedulereplay.ApplyCanonical,
				Domain:  tc.domain,
			})
			if err != nil {
				t.Fatalf("concurrent RunSchedule: %v", err)
			}
			if batchCalls == 0 {
				t.Fatal("concurrent run never invoked ClaimBatch; the batch path was not exercised")
			}
			if !bytes.Equal(seqSnap, concSnap) {
				t.Fatalf("concurrent snapshot differs from sequential:\n%s\n---\n%s", seqSnap, concSnap)
			}
		})
	}
}

// TestProjectionScheduleReplayCatchesCrossHookOrderingBug is the teeth proof
// required for a shared-conflict-key projection: on the
// incident_repository_correlation cassette, the work_item.external_link ->
// Incident edge (HAS_WORK_ITEM, From: Incident, To: WorkItem) crosses from the
// work_item_evidence_read_model hook that emits the edge to the
// incident_context_read_model hook that owns the Incident endpoint. Reusing
// orderSensitiveApply (defined in scenario_test.go; it drops any edge whose
// From node has not yet been applied) against reverse delivery drops both
// HAS_WORK_ITEM edges, because reversing the cassette's authored order (both
// incidents before either work item) puts every external_link fact before its
// referenced incident.record fact. In-order delivery keeps both edges. If this
// produced identical snapshots, the harness could not catch a real
// #4019-class cross-hook ordering bug on this shared-conflict-key projection.
//
// The test pins the incident cassette (projectionCases[0]) deliberately: it is
// the only one whose cross-hook edges originate From the OTHER hook's node
// (Incident), which is what a From-sensitive applier can drop. The
// supply-chain cassette's cross-hook edges all originate From the emitting
// item's own node (Finding, Suppression), so orderSensitiveApply never drops
// them and no cross-hook divergence is possible there by construction.
func TestProjectionScheduleReplayCatchesCrossHookOrderingBug(t *testing.T) {
	t.Parallel()

	items := loadProjectionItems(t, projectionCases[0].cassettePath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inOrder, err := schedulereplay.RunSchedule(ctx, schedulereplay.Config{
		Items:   schedulereplay.ScheduleInOrder(items),
		Workers: 1,
		Apply:   orderSensitiveApply,
		Domain:  reducer.DomainIncidentRepositoryCorrelation,
	})
	if err != nil {
		t.Fatalf("in-order buggy RunSchedule: %v", err)
	}
	reverse, err := schedulereplay.RunSchedule(ctx, schedulereplay.Config{
		Items:   schedulereplay.ScheduleReverse(items),
		Workers: 1,
		Apply:   orderSensitiveApply,
		Domain:  reducer.DomainIncidentRepositoryCorrelation,
	})
	if err != nil {
		t.Fatalf("reverse buggy RunSchedule: %v", err)
	}

	if bytes.Equal(inOrder, reverse) {
		t.Fatal("order-sensitive applier produced identical snapshots for the shared-conflict-key " +
			"projection; the harness cannot catch cross-hook ordering bugs (teeth missing)")
	}
}

// TestLoadProjectionWorkItemsRejectsNoCrossHookEdge proves the loader enforces
// the shared-conflict-key cassette contract from this package's AGENTS.md: a
// cassette whose facts yield no edge crossing two different hooks' node labels
// must fail loudly. Without this guard, dropping the optional cross-reference
// payload fields (global_id, linked_cve_id, linked_purl, evidence_ref) from a
// committed cassette would leave every ordering test green while the scenario
// silently degraded to a same-hook proof.
func TestLoadProjectionWorkItemsRejectsNoCrossHookEdge(t *testing.T) {
	t.Parallel()

	// Two facts from two different hooks (so the >=2-hook property holds) but
	// no cross-reference fields, so no work item carries a cross-hook edge.
	cassette := `{
  "collector": "replay_schedule",
  "schema_version": "1",
  "scopes": [
    {
      "scope_id": "replay:schedule:no-cross-hook",
      "source_system": "replay",
      "scope_kind": "incident_repository_correlation",
      "collector_kind": "replay_schedule",
      "partition_key": "replay:schedule:no-cross-hook",
      "metadata": {"purpose": "negative control: no cross-hook edge"},
      "generation_id": "cassette-no-cross-hook-gen1",
      "observed_at": "2026-07-01T00:00:00Z",
      "trigger_kind": "snapshot",
      "facts": [
        {
          "fact_kind": "incident.record",
          "stable_fact_key": "replay:schedule:no-cross-hook:incident:INC-1",
          "schema_version": "1.0.0",
          "collector_kind": "pagerduty",
          "fencing_token": 1,
          "source_confidence": "observed",
          "payload": {"provider": "pagerduty", "provider_incident_id": "INC-1"}
        },
        {
          "fact_kind": "work_item.record",
          "stable_fact_key": "replay:schedule:no-cross-hook:work_item:WI-1",
          "schema_version": "1.0.0",
          "collector_kind": "jira",
          "fencing_token": 1,
          "source_confidence": "observed",
          "payload": {"provider": "jira", "provider_work_item_id": "1", "work_item_key": "WI-1"}
        }
      ]
    }
  ]
}`
	path := filepath.Join(t.TempDir(), "no-cross-hook.json")
	if err := os.WriteFile(path, []byte(cassette), 0o600); err != nil {
		t.Fatalf("write temp cassette: %v", err)
	}

	_, err := schedulereplay.LoadProjectionWorkItems(path)
	if err == nil || !strings.Contains(err.Error(), "cross-hook") {
		t.Fatalf("want cross-hook contract error, got %v", err)
	}
}
