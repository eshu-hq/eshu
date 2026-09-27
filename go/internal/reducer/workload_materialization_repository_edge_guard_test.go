// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// The #7285 guard (review F1): with a graph reader wired, the stale
// repository-edge retract reads the repository's current DEFINES and
// repository-side EXPOSES_ENDPOINT targets first and deletes only the stale
// ones by id. A steady-state run issues no DELETE, because on NornicDB a
// zero-row relationship DELETE costs proportional to store size
// (NornicDB#296, docs/public/reference/nornicdb-pitfalls.md).

// repoEdgeGraphReader serves the guard's two reads from a fixed graph.
type repoEdgeGraphReader struct {
	defines   map[string][]string
	endpoints map[string][]string
	reads     int
}

func (r *repoEdgeGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.reads++
	source := r.endpoints
	if strings.Contains(cypher, ":DEFINES]") {
		source = r.defines
	}
	var rows []map[string]any
	for _, repoID := range params["repo_ids"].([]string) {
		for _, target := range source[repoID] {
			rows = append(rows, map[string]any{"repo_id": repoID, "target_id": target})
		}
	}
	return rows, nil
}

func runGuardedRepoEdgeRetractHandler(
	t *testing.T,
	reader *repoEdgeGraphReader,
	candidates []WorkloadCandidate,
) *recordingCypherExecutor {
	t.Helper()
	executor := &recordingCypherExecutor{}
	handler := WorkloadMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{repoEdgeRetractRepositoryFact(repoEdgeRetractRepoID, false)}},
		InputLoader:          &stubWorkloadProjectionInputLoader{candidates: candidates},
		Materializer:         NewWorkloadMaterializer(executor),
		RepositoryEdgeReader: reader,
	}
	if _, err := handler.Handle(context.Background(), repoEdgeRetractTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if reader.reads != 2 {
		t.Fatalf("guard reads = %d, want one DEFINES and one EXPOSES_ENDPOINT read", reader.reads)
	}
	return executor
}

// repoEdgeDeleteTargets returns the target ids every recorded DELETE rel
// statement over shape bound.
func repoEdgeDeleteTargets(t *testing.T, calls []recordedCypherCall, shape string) []string {
	t.Helper()
	var targets []string
	for _, call := range calls {
		if !strings.Contains(call.cypher, shape) || !strings.Contains(call.cypher, "DELETE rel") {
			continue
		}
		for _, row := range repoEdgeRetractRows(t, call.params) {
			if row["repo_id"] != repoEdgeRetractRepoID {
				t.Fatalf("delete row %#v is outside the retracting repository", row)
			}
			target, ok := row["target_id"].(string)
			if !ok {
				t.Fatalf("delete row %#v is not id-scoped; a wired reader must use the guarded delete", row)
			}
			targets = append(targets, target)
		}
	}
	return targets
}

func TestWorkloadMaterializationGuardIssuesNoDeleteInSteadyState(t *testing.T) {
	t.Parallel()

	workloadID := workloadid.NewWorkloadID(repoEdgeRetractRepoID, "payments").String()
	endpointID := stableAPIEndpointID(repoEdgeRetractRepoID, workloadID, "/v1/payments")
	executor := runGuardedRepoEdgeRetractHandler(t, &repoEdgeGraphReader{
		defines:   map[string][]string{repoEdgeRetractRepoID: {workloadID}},
		endpoints: map[string][]string{repoEdgeRetractRepoID: {endpointID}},
	}, []WorkloadCandidate{repoEdgeRetractCandidate(repoEdgeRetractRepoID, "payments")})

	for _, call := range executor.calls {
		if strings.Contains(call.cypher, "DELETE rel") &&
			(strings.Contains(call.cypher, ":DEFINES]") || strings.Contains(call.cypher, ":EXPOSES_ENDPOINT]")) {
			t.Fatalf("steady-state run issued %q; nothing is stale, so no DELETE may reach the graph", call.cypher)
		}
	}
}

func TestWorkloadMaterializationGuardDeletesOnlyStaleTargets(t *testing.T) {
	t.Parallel()

	workloadID := workloadid.NewWorkloadID(repoEdgeRetractRepoID, "payments").String()
	staleWorkload := workloadid.NewWorkloadID(repoEdgeRetractRepoID, "billing").String()
	endpointID := stableAPIEndpointID(repoEdgeRetractRepoID, workloadID, "/v1/payments")
	executor := runGuardedRepoEdgeRetractHandler(t, &repoEdgeGraphReader{
		defines:   map[string][]string{repoEdgeRetractRepoID: {workloadID, staleWorkload}},
		endpoints: map[string][]string{repoEdgeRetractRepoID: {endpointID, "endpoint:stale"}},
	}, []WorkloadCandidate{repoEdgeRetractCandidate(repoEdgeRetractRepoID, "payments")})

	// The handler's keep-list comes from the projection it just committed;
	// dropping it would put the current workload and endpoint here.
	if got := repoEdgeDeleteTargets(t, executor.calls, "-[rel:DEFINES]->"); !reflect.DeepEqual(got, []string{staleWorkload}) {
		t.Fatalf("DEFINES delete targets = %v, want only the stale %s", got, staleWorkload)
	}
	if got := repoEdgeDeleteTargets(t, executor.calls, "-[rel:EXPOSES_ENDPOINT]->"); !reflect.DeepEqual(got, []string{"endpoint:stale"}) {
		t.Fatalf("EXPOSES_ENDPOINT delete targets = %v, want only endpoint:stale", got)
	}
}

func TestWorkloadMaterializationGuardRetractsEveryTargetWhenNoCandidates(t *testing.T) {
	t.Parallel()

	executor := runGuardedRepoEdgeRetractHandler(t, &repoEdgeGraphReader{
		defines: map[string][]string{repoEdgeRetractRepoID: {"workload:b", "workload:a"}},
	}, nil)
	if got := repoEdgeDeleteTargets(t, executor.calls, "-[rel:DEFINES]->"); !reflect.DeepEqual(got, []string{"workload:a", "workload:b"}) {
		t.Fatalf("zero-candidate DEFINES delete targets = %v, want every existing target", got)
	}
	if got := repoEdgeDeleteTargets(t, executor.calls, "-[rel:EXPOSES_ENDPOINT]->"); len(got) != 0 {
		t.Fatalf("EXPOSES_ENDPOINT delete targets = %v, want none (no endpoint edge exists)", got)
	}
}

// TestDefaultRegistryWiresRepositoryEdgeReader pins the catalog seam: a
// reader on DefaultHandlers must reach the workload_materialization handler,
// or production silently runs the unguarded delete on every run.
func TestDefaultRegistryWiresRepositoryEdgeReader(t *testing.T) {
	t.Parallel()

	reader := &repoEdgeGraphReader{}
	registry, err := NewDefaultRegistry(DefaultHandlers{RepositoryEdgeReader: reader})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v", err)
	}
	def, ok := registry.Definition(DomainWorkloadMaterialization)
	if !ok {
		t.Fatal("workload_materialization is not registered")
	}
	handler, ok := def.Handler.(WorkloadMaterializationHandler)
	if !ok {
		t.Fatalf("handler = %T, want WorkloadMaterializationHandler", def.Handler)
	}
	if handler.RepositoryEdgeReader != reader {
		t.Fatalf("RepositoryEdgeReader = %#v, want the DefaultHandlers reader", handler.RepositoryEdgeReader)
	}
}
