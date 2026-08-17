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
		case 0: // applied
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

// BenchmarkDeploymentSourceGuardStats isolates the classification pass from the
// logging around it. This is where the added cost actually lives: a first
// attempt attributed it to building the log-attribute slice and guarded that on
// slog's level check, which measured as no change at all. The stats pass must
// run on every call regardless of log level, because the zero-applied warn
// needs stats.applied to decide whether to fire.
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
