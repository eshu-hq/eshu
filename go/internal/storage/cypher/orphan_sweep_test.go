package cypher

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDefaultOrphanSweepLabelsIncludesCodeStructureLabels(t *testing.T) {
	t.Parallel()

	got := make(map[string]bool)
	for _, label := range DefaultOrphanSweepLabels() {
		got[string(label)] = true
	}

	for _, want := range []string{
		"Repository",
		"Platform",
		"EvidenceArtifact",
		"File",
		"Directory",
		"Module",
	} {
		if !got[want] {
			t.Fatalf("DefaultOrphanSweepLabels() missing %s in %#v", want, got)
		}
	}
}

func TestBuildMarkOrphanNodesStatementUsesStaticLabelAndBoundedLimit(t *testing.T) {
	t.Parallel()

	stmt, ok := BuildMarkOrphanNodesStatement(OrphanSweepLabelRepository, 1_786_000_000, 25)

	if !ok {
		t.Fatal("BuildMarkOrphanNodesStatement() ok = false, want true")
	}
	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	for _, want := range []string{
		"MATCH (n:Repository)",
		"n.evidence_source IS NOT NULL",
		"n.eshu_orphan_observed_at_unix IS NULL",
		"NOT (n)--()",
		"LIMIT $limit",
		"SET n.eshu_orphan_observed_at_unix = $observed_at_unix",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("mark Cypher missing %q:\n%s", want, stmt.Cypher)
		}
	}
	if strings.Contains(stmt.Cypher, "$label") || strings.Contains(stmt.Cypher, "DETACH DELETE") {
		t.Fatalf("mark Cypher must use static labels and never detach-delete:\n%s", stmt.Cypher)
	}
	if got := stmt.Parameters["observed_at_unix"]; got != int64(1_786_000_000) {
		t.Fatalf("observed_at_unix = %#v, want int64 timestamp", got)
	}
	if got := stmt.Parameters["limit"]; got != 25 {
		t.Fatalf("limit = %#v, want 25", got)
	}
}

func TestBuildSweepOrphanNodesStatementRequiresAgedMarkerAndZeroRelationships(t *testing.T) {
	t.Parallel()

	stmt, ok := BuildSweepOrphanNodesStatement(OrphanSweepLabelRepository, 1_785_900_000, 10)

	if !ok {
		t.Fatal("BuildSweepOrphanNodesStatement() ok = false, want true")
	}
	for _, want := range []string{
		"MATCH (n:Repository)",
		"n.evidence_source IS NOT NULL",
		"n.eshu_orphan_observed_at_unix <= $cutoff_unix",
		"NOT (n)--()",
		"LIMIT $limit",
		"DELETE n",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("sweep Cypher missing %q:\n%s", want, stmt.Cypher)
		}
	}
	if strings.Contains(stmt.Cypher, "DETACH DELETE") {
		t.Fatalf("sweep Cypher must not detach-delete:\n%s", stmt.Cypher)
	}
	if got := stmt.Parameters["cutoff_unix"]; got != int64(1_785_900_000) {
		t.Fatalf("cutoff_unix = %#v, want int64 timestamp", got)
	}
}

func TestBuildCountOrphanNodesQueryIsLabelScopedAndCapped(t *testing.T) {
	t.Parallel()

	stmt, ok := BuildCountOrphanNodesQuery(OrphanSweepLabelPlatform, 1000)

	if !ok {
		t.Fatal("BuildCountOrphanNodesQuery() ok = false, want true")
	}
	for _, want := range []string{
		"MATCH (n:Platform)",
		"n.evidence_source IS NOT NULL",
		"NOT (n)--()",
		"LIMIT $limit",
		"RETURN count(n) AS orphan_count",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("count Cypher missing %q:\n%s", want, stmt.Cypher)
		}
	}
	if strings.Contains(stmt.Cypher, "$label") {
		t.Fatalf("count Cypher must not use dynamic labels:\n%s", stmt.Cypher)
	}
}

func TestBuildClearOrphanMarkerStatementClearsRelinkedNodes(t *testing.T) {
	t.Parallel()

	stmt, ok := BuildClearOrphanMarkerStatement(OrphanSweepLabelRepository, 50)

	if !ok {
		t.Fatal("BuildClearOrphanMarkerStatement() ok = false, want true")
	}
	for _, want := range []string{
		"MATCH (n:Repository)",
		"n.eshu_orphan_observed_at_unix IS NOT NULL",
		"(n)--()",
		"LIMIT $limit",
		"REMOVE n.eshu_orphan_observed_at_unix",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("clear Cypher missing %q:\n%s", want, stmt.Cypher)
		}
	}
}

func TestCodeStructureOrphanSweepStatementsUseStaticZeroRelationshipGuards(t *testing.T) {
	t.Parallel()

	for _, label := range []OrphanSweepLabel{"File", "Directory", "Module"} {
		t.Run(string(label), func(t *testing.T) {
			t.Parallel()
			statements := []struct {
				name      string
				build     func() (Statement, bool)
				wantParts []string
			}{
				{
					name: "mark",
					build: func() (Statement, bool) {
						return BuildMarkOrphanNodesStatement(label, 1_786_000_000, 25)
					},
					wantParts: []string{
						fmt.Sprintf("MATCH (n:%s)", label),
						"n.evidence_source IS NOT NULL",
						"n.eshu_orphan_observed_at_unix IS NULL",
						"NOT (n)--()",
						"LIMIT $limit",
					},
				},
				{
					name: "sweep",
					build: func() (Statement, bool) {
						return BuildSweepOrphanNodesStatement(label, 1_785_900_000, 25)
					},
					wantParts: []string{
						fmt.Sprintf("MATCH (n:%s)", label),
						"n.evidence_source IS NOT NULL",
						"n.eshu_orphan_observed_at_unix <= $cutoff_unix",
						"NOT (n)--()",
						"LIMIT $limit",
						"DELETE n",
					},
				},
				{
					name: "count",
					build: func() (Statement, bool) {
						return BuildCountOrphanNodesQuery(label, 25)
					},
					wantParts: []string{
						fmt.Sprintf("MATCH (n:%s)", label),
						"n.evidence_source IS NOT NULL",
						"NOT (n)--()",
						"LIMIT $limit",
						"RETURN count(n) AS orphan_count",
					},
				},
				{
					name: "count_aged",
					build: func() (Statement, bool) {
						return buildCountAgedOrphanNodesQuery(label, 1_785_900_000, 25)
					},
					wantParts: []string{
						fmt.Sprintf("MATCH (n:%s)", label),
						"n.evidence_source IS NOT NULL",
						"n.eshu_orphan_observed_at_unix <= $cutoff_unix",
						"NOT (n)--()",
						"LIMIT $limit",
					},
				},
				{
					name: "clear",
					build: func() (Statement, bool) {
						return BuildClearOrphanMarkerStatement(label, 25)
					},
					wantParts: []string{
						fmt.Sprintf("MATCH (n:%s)", label),
						"n.eshu_orphan_observed_at_unix IS NOT NULL",
						"(n)--()",
						"LIMIT $limit",
						"REMOVE n.eshu_orphan_observed_at_unix",
					},
				},
			}

			for _, tc := range statements {
				stmt, ok := tc.build()
				if !ok {
					t.Fatalf("%s builder returned ok=false", tc.name)
				}
				for _, want := range tc.wantParts {
					if !strings.Contains(stmt.Cypher, want) {
						t.Fatalf("%s Cypher missing %q:\n%s", tc.name, want, stmt.Cypher)
					}
				}
				for _, forbidden := range []string{
					"$label",
					"DETACH DELETE",
					"n.evidence_source <> 'projector/canonical'",
				} {
					if strings.Contains(stmt.Cypher, forbidden) {
						t.Fatalf("%s Cypher must not contain %q:\n%s", tc.name, forbidden, stmt.Cypher)
					}
				}
			}
		})
	}
}

func TestBuildOrphanSweepStatementsRejectUnknownLabels(t *testing.T) {
	t.Parallel()

	if _, ok := BuildSweepOrphanNodesStatement(OrphanSweepLabel("DynamicLabel"), 1, 1); ok {
		t.Fatal("BuildSweepOrphanNodesStatement() ok = true, want false for unknown label")
	}
}

func TestRepositoryOrphanSweepExcludesSourceLocalCanonicalRepositories(t *testing.T) {
	t.Parallel()

	statements := []Statement{}
	for _, build := range []func() (Statement, bool){
		func() (Statement, bool) {
			return BuildMarkOrphanNodesStatement(OrphanSweepLabelRepository, 1, 10)
		},
		func() (Statement, bool) {
			return BuildSweepOrphanNodesStatement(OrphanSweepLabelRepository, 1, 10)
		},
		func() (Statement, bool) {
			return BuildCountOrphanNodesQuery(OrphanSweepLabelRepository, 10)
		},
		func() (Statement, bool) {
			return buildCountAgedOrphanNodesQuery(OrphanSweepLabelRepository, 1, 10)
		},
	} {
		stmt, ok := build()
		if !ok {
			t.Fatal("repository orphan statement builder returned ok=false")
		}
		statements = append(statements, stmt)
	}

	for _, stmt := range statements {
		if !strings.Contains(stmt.Cypher, "n.evidence_source <> 'projector/canonical'") {
			t.Fatalf("repository orphan Cypher must not match source-local canonical repositories:\n%s", stmt.Cypher)
		}
	}
}

func TestRepoRelationshipUpsertStampsTargetRepositoryForFutureSweeps(t *testing.T) {
	t.Parallel()

	for _, cypher := range []string{
		canonicalDeploysFromRepoRelationshipUpsertCypher,
		canonicalRepoDependencyUpsertCypher,
		batchCanonicalRepoDependencyUpsertCypher,
	} {
		for _, want := range []string{
			"ON CREATE SET source_repo.evidence_source",
			"source_repo.generation_id",
			"ON CREATE SET target_repo.evidence_source",
			"target_repo.generation_id",
		} {
			if !strings.Contains(cypher, want) {
				t.Fatalf("repo relationship Cypher missing sweep metadata %q:\n%s", want, cypher)
			}
		}
	}
}

func TestInfrastructurePlatformUpsertStampsGenerationForFutureSweeps(t *testing.T) {
	t.Parallel()

	if !strings.Contains(canonicalInfrastructurePlatformUpsertCypher, "p.generation_id = $generation_id") {
		t.Fatalf("platform single-row Cypher missing generation_id create metadata:\n%s", canonicalInfrastructurePlatformUpsertCypher)
	}
	if !strings.Contains(batchCanonicalInfrastructurePlatformUpsertCypher, "p.generation_id = row.generation_id") {
		t.Fatalf("platform batch Cypher missing generation_id create metadata:\n%s", batchCanonicalInfrastructurePlatformUpsertCypher)
	}
}

func TestOrphanSweepStoreDefaultLabelsConvergeAcrossBoundedBatches(t *testing.T) {
	t.Parallel()

	graph := &convergingOrphanSweepGraph{
		orphanCounts: map[string]int64{
			"Repository":       1,
			"Platform":         0,
			"EvidenceArtifact": 0,
			"File":             3,
			"Directory":        2,
			"Module":           2,
		},
		agedCounts: map[string]int64{
			"Repository":       1,
			"Platform":         0,
			"EvidenceArtifact": 0,
			"File":             3,
			"Directory":        2,
			"Module":           2,
		},
	}
	store := NewOrphanSweepStore(graph, graph)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }
	policy := OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 10,
	}

	for cycle := 0; cycle < 4; cycle++ {
		result, err := store.SweepOrphanNodes(context.Background(), policy)
		if err != nil {
			t.Fatalf("SweepOrphanNodes() cycle %d error = %v", cycle, err)
		}
		if orphanSweepTestTotal(result.Deleted) == 0 {
			break
		}
	}

	for _, label := range []string{"Repository", "Platform", "EvidenceArtifact", "File", "Directory", "Module"} {
		if got := graph.remaining(label); got != 0 {
			t.Fatalf("%s remaining orphan count = %d, want converged to 0", label, got)
		}
	}
}

func TestOrphanSweepStoreUsesInjectedClockAndBoundsMutations(t *testing.T) {
	t.Parallel()

	executor := &recordingOrphanSweepExecutor{}
	reader := &countingOrphanSweepReader{
		orphanCount: 3,
		agedCount:   4,
	}
	store := NewOrphanSweepStore(executor, reader)
	store.Now = func() time.Time { return time.Unix(1_000, 0).UTC() }

	result, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		OrphanTTL:  100 * time.Second,
		BatchLimit: 2,
		CountLimit: 5,
		Labels:     []string{"Repository"},
	})
	if err != nil {
		t.Fatalf("SweepOrphanNodes() error = %v, want nil", err)
	}
	if got := result.Counts["Repository"]; got != 3 {
		t.Fatalf("Repository count = %d, want 3", got)
	}
	if got := result.Marked["Repository"]; got != 2 {
		t.Fatalf("Repository marked = %d, want bounded 2", got)
	}
	if got := result.Deleted["Repository"]; got != 2 {
		t.Fatalf("Repository deleted = %d, want bounded 2", got)
	}
	if got := len(executor.calls); got != 3 {
		t.Fatalf("executor calls = %d, want clear/mark/sweep", got)
	}
	if got := executor.calls[1].Parameters["observed_at_unix"]; got != int64(1_000) {
		t.Fatalf("mark observed_at_unix = %#v, want injected clock", got)
	}
	if got := executor.calls[2].Parameters["cutoff_unix"]; got != int64(900) {
		t.Fatalf("sweep cutoff_unix = %#v, want injected clock minus TTL", got)
	}
}

type recordingOrphanSweepExecutor struct {
	calls []Statement
}

func (e *recordingOrphanSweepExecutor) Execute(_ context.Context, stmt Statement) error {
	e.calls = append(e.calls, stmt)
	return nil
}

type convergingOrphanSweepGraph struct {
	orphanCounts map[string]int64
	agedCounts   map[string]int64
}

func (g *convergingOrphanSweepGraph) Execute(_ context.Context, stmt Statement) error {
	if !strings.Contains(stmt.Cypher, "DELETE n") {
		return nil
	}
	label, err := orphanSweepTestLabelFromCypher(stmt.Cypher)
	if err != nil {
		return err
	}
	limit := int64(orphanSweepTestLimit(stmt.Parameters))
	deleted := min(g.agedCounts[label], limit)
	g.agedCounts[label] -= deleted
	g.orphanCounts[label] -= deleted
	return nil
}

func (g *convergingOrphanSweepGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	label, err := orphanSweepTestLabelFromCypher(cypher)
	if err != nil {
		return nil, err
	}
	counts := g.orphanCounts
	if strings.Contains(cypher, "n.eshu_orphan_observed_at_unix <= $cutoff_unix") {
		counts = g.agedCounts
	}
	count := min(counts[label], int64(orphanSweepTestLimit(params)))
	return []map[string]any{{"orphan_count": count}}, nil
}

func (g *convergingOrphanSweepGraph) remaining(label string) int64 {
	return g.orphanCounts[label]
}

type countingOrphanSweepReader struct {
	orphanCount int64
	agedCount   int64
}

func (r *countingOrphanSweepReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	count := r.orphanCount
	if strings.Contains(cypher, "n.eshu_orphan_observed_at_unix <= $cutoff_unix") {
		count = r.agedCount
	}
	return []map[string]any{{"orphan_count": count}}, nil
}

func orphanSweepTestLabelFromCypher(cypher string) (string, error) {
	for _, label := range []string{"Repository", "Platform", "EvidenceArtifact", "File", "Directory", "Module"} {
		if strings.Contains(cypher, "MATCH (n:"+label+")") {
			return label, nil
		}
	}
	return "", fmt.Errorf("missing orphan sweep label in Cypher: %s", cypher)
}

func orphanSweepTestLimit(params map[string]any) int {
	limit, ok := params["limit"].(int)
	if !ok || limit <= 0 {
		return defaultOrphanSweepBatchLimit
	}
	return limit
}

func orphanSweepTestTotal(values map[string]int64) int64 {
	var total int64
	for _, value := range values {
		total += value
	}
	return total
}
