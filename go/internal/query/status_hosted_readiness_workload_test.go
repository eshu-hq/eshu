// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// leftoverGraph reads as a graph holding `leftovers` name-only Workload nodes
// that each have a scoped twin (the pre-#5385 shape), `blocked` more that have
// an instance, and a repository count for the query-readback check.
type leftoverGraph struct {
	repositoryCount int
	leftovers       int
	blocked         int
	runErr          error
}

func (g leftoverGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"count": g.repositoryCount}, nil
}

func (g leftoverGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if g.runErr != nil {
		return nil, g.runErr
	}
	ids, _ := params["ids"].([]string)
	total := g.leftovers + g.blocked
	row := func(i int) map[string]any {
		name := fmt.Sprintf("svc%05d", i) // zero-padded so id order is index order
		return map[string]any{"id": "workload:" + name, "name": name, "repo_id": "repository:" + name}
	}
	switch {
	case strings.Contains(cypher, "w.id > $after"):
		after, _ := params["after"].(string)
		limit, _ := params["limit"].(int)
		var out []map[string]any
		for i := 0; i < total && len(out) < limit; i++ {
			if r := row(i); r["id"].(string) > after {
				out = append(out, r)
			}
		}
		return out, nil
	case strings.Contains(cypher, "[r:INSTANCE_OF]"):
		var out []map[string]any
		for i := g.leftovers; i < total; i++ {
			if id := row(i)["id"].(string); slices.Contains(ids, id) {
				out = append(out, map[string]any{"id": id})
			}
		}
		return out, nil
	case strings.Contains(cypher, "$twin_ids"):
		var out []map[string]any
		for i := 0; i < total; i++ {
			r := row(i)
			out = append(out, map[string]any{"id": legacyworkload.TwinID(r["repo_id"].(string), r["name"].(string)), "repo_id": r["repo_id"]})
		}
		return out, nil
	}
	return nil, nil
}

func leftoverSnapshot() statuspkg.RawSnapshot {
	return statuspkg.RawSnapshot{
		AsOf:          time.Date(2026, 6, 9, 12, 15, 0, 0, time.UTC),
		ScopeActivity: statuspkg.ScopeActivitySnapshot{Active: 1},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []collector.InstanceSummary{
				{InstanceID: "collector:git", CollectorKind: "git", Enabled: true, ClaimsEnabled: true},
			},
		},
	}
}

func findCheck(t *testing.T, payload map[string]any, name string) (map[string]any, bool) {
	t.Helper()
	for _, c := range payload["checks"].([]any) {
		check := c.(map[string]any)
		if check["name"] == name {
			return check, true
		}
	}
	return nil, false
}

func TestHostedReadinessFailsWhenNameOnlyWorkloadsHaveAScopedTwin(t *testing.T) {
	t.Parallel()
	payload := hostedReadinessPayload(t, leftoverSnapshot(), leftoverGraph{repositoryCount: 3, leftovers: 4, blocked: 2})

	if payload["state"] != "not_ready" {
		t.Fatalf("state = %v, want not_ready; payload=%v", payload["state"], payload)
	}
	requireFailureClass(t, payload, "legacy_workload_nodes")
	check, ok := findCheck(t, payload, "workload_identity_leftovers")
	if !ok {
		t.Fatal("no workload_identity_leftovers check")
	}
	if check["state"] != "fail" || check["failure_class"] != "legacy_workload_nodes" {
		t.Fatalf("check = %v", check)
	}
	if want := "4 name-only Workload nodes with a scoped twin (pre-#5385 leftovers)"; check["detail"] != want {
		t.Fatalf("detail = %q, want %q (the two instance-bearing nodes are not leftovers)", check["detail"], want)
	}
	if want := `POST /admin/retire-legacy-workloads {"dry_run":true} on the reducer admin port; see the leftover retirement runbook`; check["next_diagnostic"] != want {
		t.Fatalf("next_diagnostic = %q", check["next_diagnostic"])
	}
}

func TestHostedReadinessPassesWhenNoLeftoversExist(t *testing.T) {
	t.Parallel()
	payload := hostedReadinessPayload(t, leftoverSnapshot(), leftoverGraph{repositoryCount: 3})
	check, ok := findCheck(t, payload, "workload_identity_leftovers")
	if !ok || check["state"] != "pass" {
		t.Fatalf("check = %v ok=%v", check, ok)
	}
	if check["detail"] != "no name-only Workload nodes with a scoped twin" {
		t.Fatalf("detail = %q", check["detail"])
	}
	if payload["state"] != "ready" {
		t.Fatalf("state = %v, want ready", payload["state"])
	}
}

func TestHostedReadinessIgnoresNameOnlyNodesWithoutATwinOrWithInstances(t *testing.T) {
	t.Parallel()
	// Only blocked nodes: nothing the retirement could clean, so no failure.
	payload := hostedReadinessPayload(t, leftoverSnapshot(), leftoverGraph{repositoryCount: 3, blocked: 3})
	if check, _ := findCheck(t, payload, "workload_identity_leftovers"); check["state"] != "pass" {
		t.Fatalf("check = %v, want pass", check)
	}
}

// TestHostedReadinessFailsClosedWhenTheDetectionReadErrors pins that a failed
// detection read is its own failure: the repository readback (RunSingle)
// passed, so query_readback reports nothing, and the leftover check must not
// disappear and let the report say ready.
func TestHostedReadinessFailsClosedWhenTheDetectionReadErrors(t *testing.T) {
	t.Parallel()
	payload := hostedReadinessPayload(t, leftoverSnapshot(), leftoverGraph{repositoryCount: 3, runErr: errors.New("bolt unavailable")})
	check, ok := findCheck(t, payload, "workload_identity_leftovers")
	if !ok || check["state"] != "fail" || check["failure_class"] != "legacy_workload_detection_unavailable" {
		t.Fatalf("check = %v ok=%v, want fail with legacy_workload_detection_unavailable", check, ok)
	}
	if strings.Contains(fmt.Sprint(check), "bolt unavailable") {
		t.Fatalf("check must not carry backend error text: %v", check)
	}
	requireFailureClass(t, payload, "legacy_workload_detection_unavailable")
	if payload["state"] != "not_ready" {
		t.Fatalf("state = %v, want not_ready", payload["state"])
	}
}

// TestHostedReadinessLeftoverCheckIsUnknownOffNeo4j pins that the detection,
// tested on Neo4j only, never runs or passes on another backend.
func TestHostedReadinessLeftoverCheckIsUnknownOffNeo4j(t *testing.T) {
	t.Parallel()
	for _, backend := range []GraphBackend{GraphBackendNornicDB, ""} {
		payload := hostedReadinessPayloadFor(t, &StatusHandler{
			Neo4j:        leftoverGraph{repositoryCount: 3, runErr: errors.New("detection must not run")},
			GraphBackend: backend,
			StatusReader: fakeStatusReader{snapshot: leftoverSnapshot()},
		})
		check, ok := findCheck(t, payload, "workload_identity_leftovers")
		if !ok || check["state"] != "unknown" || !strings.Contains(check["detail"].(string), "Neo4j only") {
			t.Fatalf("backend %q: check = %v ok=%v, want unknown naming Neo4j only", backend, check, ok)
		}
	}
}

// TestHostedReadinessLeftoverCheckIsUnknownWhenTheCapHidesTheRest pins that a
// read which examined its cap and found no leftover does not pass: the nodes
// past the cap were never examined.
func TestHostedReadinessLeftoverCheckIsUnknownWhenTheCapHidesTheRest(t *testing.T) {
	t.Parallel()
	payload := hostedReadinessPayload(t, leftoverSnapshot(), leftoverGraph{repositoryCount: 3, blocked: hostedReadinessWorkloadLeftoversExamined + 1})
	check, ok := findCheck(t, payload, "workload_identity_leftovers")
	if !ok || check["state"] != "unknown" {
		t.Fatalf("check = %v ok=%v, want unknown", check, ok)
	}
}
