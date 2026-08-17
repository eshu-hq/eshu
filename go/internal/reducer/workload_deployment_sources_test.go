// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestApplyResolvedDeploymentSourcesReclassifiesControllerCandidateAsService(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:         "repo-api",
			RepoName:       "api-service",
			Classification: "utility",
			Confidence:     0.42,
			Provenance:     []string{"jenkins_pipeline"},
		},
	}
	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-deploy",
			TargetRepoID:     "repo-api",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.9,
			ResolutionSource: "test",
			Details: map[string]any{
				"evidence_kinds": []string{string(relationships.EvidenceKindKustomizeResource)},
			},
		},
	}

	enriched := applyResolvedDeploymentSources(candidates, resolved)
	if len(enriched) != 1 {
		t.Fatalf("len(enriched) = %d, want 1", len(enriched))
	}
	candidate := enriched[0]
	if got, want := candidate.DeploymentRepoID, "repo-deploy"; got != want {
		t.Fatalf("DeploymentRepoID = %q, want %q", got, want)
	}
	if got, want := candidate.Classification, "service"; got != want {
		t.Fatalf("Classification = %q, want %q after deployment evidence enrichment", got, want)
	}
	if got, want := candidate.Confidence, 0.9; got != want {
		t.Fatalf("Confidence = %f, want %f", got, want)
	}
	if !hasProvenance(candidate.Provenance, "kustomize_resource") {
		t.Fatalf("Provenance = %v, want kustomize_resource", candidate.Provenance)
	}
}

func TestApplyResolvedDeploymentSourcesPreservesMultipleDeploymentRepos(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID:         "repo-api",
			RepoName:       "api-service",
			Classification: "service",
			Confidence:     0.84,
			Provenance:     []string{"dockerfile_runtime"},
		},
	}
	resolved := []relationships.ResolvedRelationship{
		{
			SourceRepoID:     "repo-current-deploy",
			TargetRepoID:     "repo-api",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.90,
			Details: map[string]any{
				"evidence_kinds": []string{string(relationships.EvidenceKindHelmValues)},
			},
		},
		{
			SourceRepoID:     "repo-api",
			TargetRepoID:     "repo-next-deploy",
			RelationshipType: relationships.RelDeploysFrom,
			Confidence:       0.93,
			Details: map[string]any{
				"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDApplicationSetDeploySource)},
			},
		},
	}

	enriched := applyResolvedDeploymentSources(candidates, resolved)
	if len(enriched) != 1 {
		t.Fatalf("len(enriched) = %d, want 1", len(enriched))
	}
	candidate := enriched[0]
	if got, want := candidate.DeploymentRepoID, "repo-next-deploy"; got != want {
		t.Fatalf("DeploymentRepoID = %q, want highest-confidence primary %q", got, want)
	}
	if got, want := candidate.DeploymentRepoIDs, []string{"repo-current-deploy", "repo-next-deploy"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("DeploymentRepoIDs = %#v, want %#v", got, want)
	}
	if got, want := candidate.Confidence, 0.93; got != want {
		t.Fatalf("Confidence = %f, want %f", got, want)
	}
	if !hasProvenance(candidate.Provenance, "helm_deployment") {
		t.Fatalf("Provenance = %v, want helm_deployment", candidate.Provenance)
	}
	if !hasProvenance(candidate.Provenance, "argocd_applicationset_deploy_source") {
		t.Fatalf("Provenance = %v, want argocd_applicationset_deploy_source", candidate.Provenance)
	}
}

// deploymentSourceRelationshipOutcome names which of applyResolvedDeploymentSources's
// three guards a resolved relationship trips, or "applied" if it survives all
// three -- the same classification the enrichment loop uses to decide
// skip-vs-apply, so a zero-edges materialization can be diagnosed from one
// log line instead of the guard being reconstructed by hand (#6149 follow-up
// item 6). One case per guard, in the same order the loop checks them, plus
// the applied case.
func TestDeploymentSourceRelationshipOutcome(t *testing.T) {
	t.Parallel()

	deploysFromWithEvidence := relationships.ResolvedRelationship{
		SourceRepoID:     "repo-deploy",
		TargetRepoID:     "repo-api",
		RelationshipType: relationships.RelDeploysFrom,
		Details: map[string]any{
			"evidence_kinds": []string{string(relationships.EvidenceKindKustomizeResource)},
		},
	}

	cases := []struct {
		name         string
		relationship relationships.ResolvedRelationship
		want         string
	}{
		{
			name: "wrong relationship type",
			relationship: func() relationships.ResolvedRelationship {
				r := deploysFromWithEvidence
				r.RelationshipType = relationships.RelProvisionsDependencyFor
				return r
			}(),
			want: "wrong_type",
		},
		{
			name: "missing source repo id",
			relationship: func() relationships.ResolvedRelationship {
				r := deploysFromWithEvidence
				r.SourceRepoID = ""
				return r
			}(),
			want: "missing_repo_id",
		},
		{
			name: "missing target repo id",
			relationship: func() relationships.ResolvedRelationship {
				r := deploysFromWithEvidence
				r.TargetRepoID = ""
				return r
			}(),
			want: "missing_repo_id",
		},
		{
			name: "no recognized deployment evidence",
			relationship: func() relationships.ResolvedRelationship {
				r := deploysFromWithEvidence
				r.Details = map[string]any{"evidence_kinds": []string{"some_other_kind"}}
				return r
			}(),
			want: "no_deployment_evidence",
		},
		{
			name:         "applied",
			relationship: deploysFromWithEvidence,
			want:         "applied",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := deploymentSourceRelationshipOutcome(tc.relationship); got != tc.want {
				t.Errorf("deploymentSourceRelationshipOutcome() = %q, want %q", got, tc.want)
			}
		})
	}
}

// deploymentSourceGuardStats tallies deploymentSourceRelationshipOutcome
// across a batch of resolved relationships -- the aggregate a caller with a
// context (unlike applyResolvedDeploymentSources and
// ExtractDeployableUnitCorrelationRows, which stay side-effect-free for
// Ifá's direct-call contract) logs to name which guard actually produced a
// zero-edges outcome.
func TestDeploymentSourceGuardStats(t *testing.T) {
	t.Parallel()

	resolved := []relationships.ResolvedRelationship{
		{RelationshipType: relationships.RelProvisionsDependencyFor},                                 // wrong_type
		{RelationshipType: relationships.RelDeploysFrom, SourceRepoID: "", TargetRepoID: "repo-api"}, // missing_repo_id
		{
			RelationshipType: relationships.RelDeploysFrom, SourceRepoID: "repo-deploy", TargetRepoID: "repo-api",
			Details: map[string]any{"evidence_kinds": []string{"unrecognized"}},
		}, // no_deployment_evidence
		{
			RelationshipType: relationships.RelDeploysFrom, SourceRepoID: "repo-deploy", TargetRepoID: "repo-api",
			Details: map[string]any{"evidence_kinds": []string{string(relationships.EvidenceKindHelmValues)}},
		}, // applied
	}

	stats := deploymentSourceGuardStats(resolved)
	if stats.total != 4 {
		t.Errorf("total = %d, want 4", stats.total)
	}
	if stats.wrongType != 1 {
		t.Errorf("wrongType = %d, want 1", stats.wrongType)
	}
	if stats.missingRepoID != 1 {
		t.Errorf("missingRepoID = %d, want 1", stats.missingRepoID)
	}
	if stats.noEvidence != 1 {
		t.Errorf("noEvidence = %d, want 1", stats.noEvidence)
	}
	if stats.applied != 1 {
		t.Errorf("applied = %d, want 1", stats.applied)
	}
}

// logDeploymentSourceGuardStats warns, not merely informs, when resolved is
// non-empty but zero relationships were applied -- the actual "zero edges"
// failure mode #6149 follow-up item 6 exists to make diagnosable. No
// t.Parallel(): this test swaps slog's process-global default logger
// (mirrors TestSemanticEntityMaterializationHandlerLogsStageTiming's
// established pattern in this package), which is only safe while no other
// test in this binary is concurrently logging.
func TestLogDeploymentSourceGuardStatsWarnsOnZeroApplied(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	resolved := []relationships.ResolvedRelationship{
		{
			RelationshipType: relationships.RelDeploysFrom, SourceRepoID: "repo-deploy", TargetRepoID: "repo-api",
			Details: map[string]any{"evidence_kinds": []string{"unrecognized"}},
		}, // no_deployment_evidence
	}
	logDeploymentSourceGuardStats(context.Background(), "workload_projection", "scope-1", "generation-1", resolved)

	logText := logs.String()
	for _, want := range []string{
		`"level":"WARN"`,
		`"msg":"deployment source resolution applied zero relationships"`,
		`"domain":"workload_projection"`,
		`"scope_id":"scope-1"`,
		`"generation_id":"generation-1"`,
		`"resolved_total":1`,
		`"skipped_no_deployment_evidence":1`,
		`"applied":0`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}

// The routine case (at least one relationship applied) logs at debug level,
// not warn -- the vast majority of resolved relationships in any batch are
// legitimately not deployment-source relationships, so warning on every call
// would be noise rather than signal.
func TestLogDeploymentSourceGuardStatsDebugsWhenApplied(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	resolved := []relationships.ResolvedRelationship{
		{RelationshipType: relationships.RelProvisionsDependencyFor}, // wrong_type
		{
			RelationshipType: relationships.RelDeploysFrom, SourceRepoID: "repo-deploy", TargetRepoID: "repo-api",
			Details: map[string]any{"evidence_kinds": []string{string(relationships.EvidenceKindHelmValues)}},
		}, // applied
	}
	logDeploymentSourceGuardStats(context.Background(), "workload_projection", "scope-1", "generation-1", resolved)

	logText := logs.String()
	for _, want := range []string{
		`"level":"DEBUG"`,
		`"msg":"deployment source resolution guard stats"`,
		`"resolved_total":2`,
		`"skipped_wrong_type":1`,
		`"applied":1`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
	if strings.Contains(logText, `"level":"WARN"`) {
		t.Fatalf("expected no WARN line when at least one relationship was applied:\n%s", logText)
	}
}

// An empty resolved slice logs nothing at all -- applyResolvedDeploymentSources's
// own no-op fast path for len(resolved)==0, mirrored here so the two never
// silently diverge (an empty batch is not a guard firing; it is nothing to
// diagnose).
func TestLogDeploymentSourceGuardStatsNoOpOnEmptyResolved(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	logDeploymentSourceGuardStats(context.Background(), "workload_projection", "scope-1", "generation-1", nil)

	if logs.Len() != 0 {
		t.Fatalf("expected no log output for an empty resolved slice, got:\n%s", logs.String())
	}
}
