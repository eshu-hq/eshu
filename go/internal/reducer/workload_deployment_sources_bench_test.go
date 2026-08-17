// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// benchResolvedRelationships builds a resolved set with a realistic spread
// across deploymentSourceRelationshipOutcome's four verdicts, so the switch in
// deploymentSourceGuardStats is not measured against a single hot branch.
func benchResolvedRelationships(n int) []relationships.ResolvedRelationship {
	resolved := make([]relationships.ResolvedRelationship, 0, n)
	for i := range n {
		switch i % 4 {
		case 0: // passes all three guards
			resolved = append(resolved, relationships.ResolvedRelationship{
				RelationshipType: relationships.RelDeploysFrom,
				SourceRepoID:     "repo-deploy",
				TargetRepoID:     "repo-api",
				Details:          map[string]any{"evidence_kinds": []string{"workflow_deploy_step"}},
			})
		case 1: // wrong type
			resolved = append(resolved, relationships.ResolvedRelationship{
				RelationshipType: relationships.RelDependsOn,
				SourceRepoID:     "repo-deploy",
				TargetRepoID:     "repo-api",
			})
		case 2: // missing repo id
			resolved = append(resolved, relationships.ResolvedRelationship{
				RelationshipType: relationships.RelDeploysFrom,
				SourceRepoID:     "repo-deploy",
				Details:          map[string]any{"evidence_kinds": []string{"workflow_deploy_step"}},
			})
		default: // no deployment evidence
			resolved = append(resolved, relationships.ResolvedRelationship{
				RelationshipType: relationships.RelDeploysFrom,
				SourceRepoID:     "repo-deploy",
				TargetRepoID:     "repo-api",
				Details:          map[string]any{"evidence_kinds": []string{"unrecognized"}},
			})
		}
	}
	return resolved
}

// benchResolvedRelationshipsWrongTypeDominated is the shape a real batch has:
// the overwhelming majority of resolved relationships are not deployment-source
// relationships at all.
//
// It is also the shape that DISPROVED the prediction it was built to test. The
// prediction: applyResolvedDeploymentSources calls the SAME classifier and then
// `continue`s on anything not applied, so as the applied fraction falls the
// baseline should degenerate toward what the stats pass does and the added-cost
// ratio should rise toward 1. Measured, the ratio is nearly flat and falls
// slightly -- ~85% at 25% applied, ~81% at 5%. The reasoning stopped one line
// short of workload_deployment_sources.go:158, which allocates
// make(map, len(resolved)) BEFORE the loop: 21800 B and 3 allocs at either
// shape, a fixed cost the stats pass never pays, so the baseline never
// degenerates. Both passes do get cheaper on wrong_type-dominated input,
// because the classifier short-circuits before touching Details.
//
// Kept because a benchmark that refutes a plausible reading of the code is
// worth more than one that confirms an obvious result. See the hypothesis
// ledger in evidence-6149-deployment-source-guard-stats.md.
func benchResolvedRelationshipsWrongTypeDominated(n int) []relationships.ResolvedRelationship {
	resolved := make([]relationships.ResolvedRelationship, 0, n)
	for i := range n {
		if i%20 == 0 { // 5% applied
			resolved = append(resolved, relationships.ResolvedRelationship{
				RelationshipType: relationships.RelDeploysFrom,
				SourceRepoID:     "repo-deploy",
				TargetRepoID:     "repo-api",
				Details:          map[string]any{"evidence_kinds": []string{"workflow_deploy_step"}},
			})
			continue
		}
		resolved = append(resolved, relationships.ResolvedRelationship{
			RelationshipType: relationships.RelDependsOn,
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-api",
		})
	}
	return resolved
}

// BenchmarkApplyResolvedDeploymentSources measures the pre-existing pass over
// the resolved set -- the baseline the guard-stats pass is added alongside.
func BenchmarkApplyResolvedDeploymentSources(b *testing.B) {
	resolved := benchResolvedRelationships(256)
	candidates := []WorkloadCandidate{{RepoID: "repo-deploy"}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = applyResolvedDeploymentSources(candidates, resolved)
	}
}

// BenchmarkApplyResolvedDeploymentSourcesWrongTypeDominated is the same
// baseline on the realistic shape. Compare against
// BenchmarkDeploymentSourceGuardStatsWrongTypeDominated to see the ratio at the
// applied fraction production actually has.
func BenchmarkApplyResolvedDeploymentSourcesWrongTypeDominated(b *testing.B) {
	resolved := benchResolvedRelationshipsWrongTypeDominated(256)
	candidates := []WorkloadCandidate{{RepoID: "repo-deploy"}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = applyResolvedDeploymentSources(candidates, resolved)
	}
}

// BenchmarkDeploymentSourceGuardStatsWrongTypeDominated isolates the added pass
// on the realistic shape.
func BenchmarkDeploymentSourceGuardStatsWrongTypeDominated(b *testing.B) {
	resolved := benchResolvedRelationshipsWrongTypeDominated(256)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = deploymentSourceGuardStats(resolved)
	}
}

// BenchmarkDeploymentSourceGuardStats isolates the classification pass from the
// logging around it. This is where the added cost actually lives: a first
// attempt attributed it to building the log-attribute slice and guarded that on
// slog's level check, which measured as no change at all. The stats pass must
// run on every call regardless of log level, because the warn condition needs
// stats.passedGuards and stats.wrongType/stats.total to decide whether to fire.
func BenchmarkDeploymentSourceGuardStats(b *testing.B) {
	resolved := benchResolvedRelationships(256)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = deploymentSourceGuardStats(resolved)
	}
}

// BenchmarkLogDeploymentSourceGuardStatsDebugDisabled measures the added cost
// on the normal production path, where the default logger filters Debug. The
// attribute slice is built before the slog call, so this is NOT free -- it is
// the number the No-Regression claim rests on.
func BenchmarkLogDeploymentSourceGuardStatsDebugDisabled(b *testing.B) {
	resolved := benchResolvedRelationships(256)
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previous)

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		logDeploymentSourceGuardStats(ctx, "workload_projection", "scope-1", "generation-1", resolved)
	}
}

// BenchmarkLogDeploymentSourceGuardStatsWrongTypeDominated measures the whole
// added call -- classification plus attribute slice plus slog -- on the
// realistic shape, so the added-vs-baseline ratio can be taken from two numbers
// gathered the same way rather than assembled from different shapes.
func BenchmarkLogDeploymentSourceGuardStatsWrongTypeDominated(b *testing.B) {
	resolved := benchResolvedRelationshipsWrongTypeDominated(256)
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previous)

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		logDeploymentSourceGuardStats(ctx, "workload_projection", "scope-1", "generation-1", resolved)
	}
}

// BenchmarkLogDeploymentSourceGuardStatsDebugEnabled measures the cost when an
// operator has turned Debug on -- the diagnostic case the line exists for.
func BenchmarkLogDeploymentSourceGuardStatsDebugEnabled(b *testing.B) {
	resolved := benchResolvedRelationships(256)
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		logDeploymentSourceGuardStats(ctx, "workload_projection", "scope-1", "generation-1", resolved)
	}
}
