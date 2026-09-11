// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"testing"
)

func TestAddSupplyChainRuntimeContextFactWorkloadIdentity(t *testing.T) {
	t.Parallel()

	out := map[string]SupplyChainRuntimeContext{}
	AddSupplyChainRuntimeContextFact(out, WorkloadIdentityFactKindQuery, "repository:r_217415d9", map[string]any{
		"entity_keys": []any{"workload:supply-chain-demo-db", "workload:supply-chain-demo-db-worker"},
	})
	ctx, ok := out["repository:r_217415d9"]
	if !ok {
		t.Fatal("no context decoded for repository:r_217415d9")
	}
	if len(ctx.WorkloadIDs) != 2 {
		t.Errorf("WorkloadIDs = %v, want 2 workloads from entity_keys", ctx.WorkloadIDs)
	}
}

func TestAddSupplyChainRuntimeContextFactWorkloadIdentityNormalizesReducerShapes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		payload    map[string]any
		workloadID string
	}{
		{
			name: "padded scalar workload",
			payload: map[string]any{
				"workload_id": "  workload:padded-scalar  ",
			},
			workloadID: "workload:padded-scalar",
		},
		{
			name: "padded entity-key array",
			payload: map[string]any{
				"entity_keys": []any{"  workload:padded-array  "},
			},
			workloadID: "workload:padded-array",
		},
		{
			name: "scalar entity key",
			payload: map[string]any{
				"entity_keys": "  workload:scalar-entity-key  ",
			},
			workloadID: "workload:scalar-entity-key",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.payload["repository_id"] = "repository:r_normalized"
			out := map[string]SupplyChainRuntimeContext{}
			AddSupplyChainRuntimeContextFact(
				out,
				WorkloadIdentityFactKindQuery,
				"scope",
				tc.payload,
			)
			got := out["repository:r_normalized"].WorkloadIDs
			if len(got) != 1 || got[0] != tc.workloadID {
				t.Fatalf("WorkloadIDs = %v, want [%s]", got, tc.workloadID)
			}
		})
	}
}

func TestAddSupplyChainRuntimeContextFactWorkloadIdentityRejectsObjectEntityKeys(t *testing.T) {
	t.Parallel()

	out := map[string]SupplyChainRuntimeContext{}
	AddSupplyChainRuntimeContextFact(
		out,
		WorkloadIdentityFactKindQuery,
		"scope",
		map[string]any{
			"repository_id": "repository:r_object",
			"entity_keys": map[string]any{
				"workload:object-key": true,
			},
		},
	)
	if workloads := out["repository:r_object"].WorkloadIDs; len(workloads) != 0 {
		t.Fatalf("WorkloadIDs = %v, want object-shaped entity_keys ignored", workloads)
	}
}

func TestAddSupplyChainRuntimeContextFactServiceSkipsRejectedOutcome(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{"ambiguous", "rejected", "unresolved", "stale"} {
		outcome := outcome
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			out := map[string]SupplyChainRuntimeContext{}
			AddSupplyChainRuntimeContextFact(out, serviceCatalogCorrelationFactKind, "scope", map[string]any{
				"repository_id": "repository:r_217415d9",
				"service_id":    "service:demo-db",
				"outcome":       outcome,
			})
			if _, ok := out["repository:r_217415d9"]; ok {
				t.Errorf("outcome %q should not resolve context", outcome)
			}
		})
	}
}

func TestAddSupplyChainRuntimeContextFactServiceSkipsProvenanceOnly(t *testing.T) {
	t.Parallel()

	for _, provenanceOnly := range []any{true, " TRUE "} {
		out := map[string]SupplyChainRuntimeContext{}
		AddSupplyChainRuntimeContextFact(out, serviceCatalogCorrelationFactKind, "scope", map[string]any{
			"repository_id":   "repository:r_217415d9",
			"service_id":      "service:demo-db",
			"provenance_only": provenanceOnly,
		})
		if _, ok := out["repository:r_217415d9"]; ok {
			t.Errorf("provenance_only=%#v should not resolve context", provenanceOnly)
		}
	}
}

func TestAddSupplyChainRuntimeContextFactServiceAcceptsFalseOrBlankProvenance(t *testing.T) {
	t.Parallel()

	for _, provenanceOnly := range []any{false, " false ", " ", nil} {
		out := map[string]SupplyChainRuntimeContext{}
		payload := map[string]any{
			"repository_id": "repository:r_217415d9",
			"service_id":    "service:demo-db",
			"outcome":       "exact",
		}
		if provenanceOnly != nil {
			payload["provenance_only"] = provenanceOnly
		}
		AddSupplyChainRuntimeContextFact(
			out,
			serviceCatalogCorrelationFactKind,
			"scope",
			payload,
		)
		if _, ok := out["repository:r_217415d9"]; !ok {
			t.Errorf("provenance_only=%#v should resolve context", provenanceOnly)
		}
	}
}

func TestAddSupplyChainRuntimeContextFactServiceExactDerivedAndEmptyOutcome(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{"exact", "derived", ""} {
		outcome := outcome
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			out := map[string]SupplyChainRuntimeContext{}
			AddSupplyChainRuntimeContextFact(out, serviceCatalogCorrelationFactKind, "scope", map[string]any{
				"repository_id": "repository:r_217415d9",
				"service_id":    "service:demo-db",
				"workload_id":   "workload:supply-chain-demo-db",
				"entity_ref":    "component:demo-db",
				"owner_ref":     "group:owners",
				"outcome":       outcome,
			})
			ctx, ok := out["repository:r_217415d9"]
			if !ok {
				t.Fatalf("outcome %q should resolve context", outcome)
			}
			if len(ctx.ServiceIDs) != 1 || len(ctx.WorkloadIDs) != 1 || len(ctx.CatalogEntityRefs) != 1 || len(ctx.CatalogOwnerRefs) != 1 {
				t.Errorf("context = %+v, want 1 service/1 workload/1 entity/1 owner", ctx)
			}
		})
	}
}

func TestAddSupplyChainRuntimeContextFactPlatformDeployments(t *testing.T) {
	t.Parallel()

	// Live corpus shape: platform_materialization carries deployments under
	// entity_keys. Mirror the reducer exactly (supplyChainDeploymentIDsFromPayload):
	// singular deployment_id first, then entity_keys filtered to
	// deployment:-prefixed keys — replay/fallback intent paths can persist
	// repo:, platform:, aws:, tfstate:, cloud:, or canonical fact-id strings
	// into entity_keys, and those must never surface as deployment anchors.
	out := map[string]SupplyChainRuntimeContext{}
	AddSupplyChainRuntimeContextFact(out, PlatformMaterializationFactKindQuery, "git-repository-scope:repository:r_217415d9", map[string]any{
		"entity_keys": []any{"deployment:deployable-config", "repo:some-repo", "platform:some-platform", "canonical:platform_materialization:abc"},
	})
	ctx := out["repository:r_217415d9"]
	if len(ctx.DeploymentIDs) != 1 || ctx.DeploymentIDs[0] != "deployment:deployable-config" {
		t.Errorf("DeploymentIDs = %v, want only [deployment:deployable-config] (non-deployment entity_keys filtered)", ctx.DeploymentIDs)
	}

	out = map[string]SupplyChainRuntimeContext{}
	AddSupplyChainRuntimeContextFact(out, PlatformMaterializationFactKindQuery, "scope", map[string]any{
		"repository_id":  "repository:r_217415d9",
		"deployment_id":  "deployment:demo-db-prod",
		"deployment_ids": []any{"deployment:wrong-key-shape"},
		"entity_keys":    []any{"deployment:demo-db-staging"},
	})
	ctx = out["repository:r_217415d9"]
	if len(ctx.DeploymentIDs) != 2 {
		t.Fatalf("DeploymentIDs = %v, want 2 (singular deployment_id + deployment:-prefixed entity key)", ctx.DeploymentIDs)
	}
	for _, id := range ctx.DeploymentIDs {
		if id == "deployment:wrong-key-shape" {
			t.Error("deployment_ids plural key must not be read (no writer emits it)")
		}
	}
}

func TestAddSupplyChainRuntimeContextFactCICDEnvironment(t *testing.T) {
	t.Parallel()

	out := map[string]SupplyChainRuntimeContext{}
	AddSupplyChainRuntimeContextFact(out, cicdRunCorrelationFactKind, "scope", map[string]any{
		"repository_id": "repository:r_217415d9",
		"environment":   "production",
		"outcome":       "exact",
	})
	ctx := out["repository:r_217415d9"]
	if len(ctx.Environments) != 1 || ctx.Environments[0] != "production" {
		t.Errorf("Environments = %v, want [production]", ctx.Environments)
	}
}

func TestSupplyChainRuntimeContextRepositoryIDRejectsOCIPath(t *testing.T) {
	t.Parallel()

	// The reducer accepts a direct payload repository_id verbatim — including
	// an OCI registry path — but an OCI path can never match a finding's git
	// repository candidate, so the SQL never selects such a fact for a git
	// candidate set. This test pins the mirror, not a rejection gate.
	if got := supplyChainRuntimeContextRepositoryID(map[string]any{
		"repository_id": "oci-registry://registry.example.com/demo",
	}, ""); got != "oci-registry://registry.example.com/demo" {
		t.Errorf("repositoryID = %q, want verbatim direct payload id (mirror of reducer firstNonBlank direct)", got)
	}
}

func TestSupplyChainRuntimeContextRepositoryIDFromGitScope(t *testing.T) {
	t.Parallel()

	if got := supplyChainRuntimeContextRepositoryID(map[string]any{}, "git-repository-scope:repository:r_217415d9"); got != "repository:r_217415d9" {
		t.Errorf("repositoryID = %q, want repository:r_217415d9", got)
	}
}

func TestSupplyChainRuntimeContextRepositoryIDAcceptsNonPrefixedDirectID(t *testing.T) {
	t.Parallel()

	// Mirror the reducer (supplyChainWorkloadRepositoryID): a direct payload
	// repository_id/repo_id is accepted verbatim — consumption-derived anchors
	// use non-prefixed forms like github.com/org/repo or repo://acme/api, and
	// rejecting them leaves an honest-empty runtime_context for facts the SQL
	// already matched.
	for _, key := range []string{"repository_id", "repo_id"} {
		key := key
		for _, id := range []string{"github.com/org/repo", "repo://acme/api", "repository:r_217415d9"} {
			id := id
			t.Run(key+"/"+id, func(t *testing.T) {
				t.Parallel()
				payload := map[string]any{
					key:                 id,
					"scope_id":          "repository:r_decoy",
					"related_scope_ids": []any{"repository:r_related"},
				}
				if got := supplyChainRuntimeContextRepositoryID(payload, "repository:r_envelope"); got != id {
					t.Errorf("repositoryID = %q, want direct %s %q", got, key, id)
				}
			})
		}
	}
}

func TestSupplyChainRuntimeContextRepositoryIDFromRelatedScopeIDs(t *testing.T) {
	t.Parallel()

	// Mirror the reducer: a fact scoped to a non-repository scope can carry
	// its repository anchor only in related_scope_ids — scan it for both
	// repository: and git-repository-scope:-prefixed entries.
	if got := supplyChainRuntimeContextRepositoryID(map[string]any{
		"related_scope_ids": []any{"scan-target-xyz", "git-repository-scope:repository:r_217415d9"},
	}, "scan-target-xyz"); got != "repository:r_217415d9" {
		t.Errorf("repositoryID = %q, want repository:r_217415d9 from related_scope_ids", got)
	}
}

func TestSupplyChainRuntimeContextRepositoryIDMatchesReducerScopePrecedence(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		payload  map[string]any
		scopeID  string
		expected string
	}{
		{
			name: "raw payload scope shadows prefixed envelope scope",
			payload: map[string]any{
				"scope_id": "github.com/example/repo-a",
			},
			scopeID:  "repository:r_decoy",
			expected: "github.com/example/repo-a",
		},
		{
			name: "related repository beats raw selected scope fallback",
			payload: map[string]any{
				"scope_id":          "github.com/example/repo-a",
				"related_scope_ids": []any{"repository:r_related"},
			},
			scopeID:  "repository:r_decoy",
			expected: "repository:r_related",
		},
		{
			name:     "raw envelope scope is the final fallback",
			payload:  map[string]any{"scope_id": "  "},
			scopeID:  "github.com/example/repo-envelope",
			expected: "github.com/example/repo-envelope",
		},
		{
			name: "related array entries are trimmed",
			payload: map[string]any{
				"scope_id":          "github.com/example/repo-a",
				"related_scope_ids": []any{"  repository:r_whitespace  "},
			},
			scopeID:  "repository:r_decoy",
			expected: "repository:r_whitespace",
		},
		{
			name: "scalar related scope is accepted",
			payload: map[string]any{
				"scope_id":          "github.com/example/repo-a",
				"related_scope_ids": "  repository:r_scalar  ",
			},
			scopeID:  "repository:r_decoy",
			expected: "repository:r_scalar",
		},
		{
			name: "malformed related scope does not mask later valid scope",
			payload: map[string]any{
				"scope_id": "github.com/example/repo-a",
				"related_scope_ids": []any{
					"git-repository-scope:   ",
					"  repository:r_later  ",
				},
			},
			scopeID:  "repository:r_decoy",
			expected: "repository:r_later",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := supplyChainRuntimeContextRepositoryID(tc.payload, tc.scopeID); got != tc.expected {
				t.Fatalf("repositoryID = %q, want reducer-equivalent %q", got, tc.expected)
			}
		})
	}
}

func TestAddSupplyChainRuntimeContextFactFallsBackToRawScope(t *testing.T) {
	t.Parallel()

	out := map[string]SupplyChainRuntimeContext{}
	AddSupplyChainRuntimeContextFact(out, WorkloadIdentityFactKindQuery, "scan-target-xyz", map[string]any{
		"entity_keys": []any{"workload:x"},
	})
	ctx, ok := out["scan-target-xyz"]
	if !ok || len(ctx.WorkloadIDs) != 1 || ctx.WorkloadIDs[0] != "workload:x" {
		t.Errorf("out = %v, want workload context under reducer raw-scope fallback", out)
	}
}

func TestAddSupplyChainRuntimeContextFactIgnoresBlankScope(t *testing.T) {
	t.Parallel()

	out := map[string]SupplyChainRuntimeContext{}
	AddSupplyChainRuntimeContextFact(out, WorkloadIdentityFactKindQuery, "", map[string]any{
		"entity_keys": []any{"workload:x"},
	})
	if len(out) != 0 {
		t.Errorf("out = %v, want no context without any repository or scope anchor", out)
	}
}

func TestAddSupplyChainRuntimeContextFactWorkloadIDPayloadAndPrefixFilter(t *testing.T) {
	t.Parallel()

	// Mirror the reducer (supplyChainWorkloadIDsFromPayload): payload
	// workload_id first, then entity_keys filtered to workload:-prefixed keys —
	// a non-workload entity key must never become runtime context.
	out := map[string]SupplyChainRuntimeContext{}
	AddSupplyChainRuntimeContextFact(out, WorkloadIdentityFactKindQuery, "repository:r_217415d9", map[string]any{
		"workload_id": "workload:primary",
		"entity_keys": []any{"workload:secondary", "deployment:not-a-workload", "service:also-not"},
	})
	ctx := out["repository:r_217415d9"]
	if len(ctx.WorkloadIDs) != 2 {
		t.Fatalf("WorkloadIDs = %v, want exactly 2 (payload workload_id + workload:-prefixed entity key)", ctx.WorkloadIDs)
	}
	for _, id := range ctx.WorkloadIDs {
		if id != "workload:primary" && id != "workload:secondary" {
			t.Errorf("WorkloadIDs contains non-workload entry %q", id)
		}
	}
}

func TestListSupplyChainImpactRuntimeContextNilDBFailsLoud(t *testing.T) {
	t.Parallel()

	// A nil-DB store must fail loud like the sibling list read — a silent
	// honest-empty on every finding reads as "nothing runs this" to a caller.
	store := PostgresSupplyChainImpactFindingStore{}
	_, err := store.ListSupplyChainImpactRuntimeContext(
		context.Background(),
		[]string{"repository:r_217415d9"},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("ListSupplyChainImpactRuntimeContext() error = nil, want nil-DB error")
	}
	if got := err.Error(); got != "supply chain impact runtime context database is required" {
		t.Errorf("error = %q, want %q", got, "supply chain impact runtime context database is required")
	}
}

func TestRuntimeEnvironmentEvidenceQueryUsesCurrentAuthorizedExactPairs(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"candidate_pairs AS MATERIALIZED",
		"UNNEST($1::text[], $2::text[])",
		"CROSS JOIN LATERAL",
		"fact.payload->>'artifact_digest' = candidate.digest",
		"fact.is_tombstone = FALSE",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"runtime_repository.repository_id = ANY($3::text[])",
		"fact.scope_id = ANY($4::text[])",
		"BOOL_OR",
		"HAVING COUNT(*) > 0",
		"ORDER BY candidate.digest, candidate.environment",
	} {
		if !strings.Contains(selectSupplyChainImpactRuntimeEnvironmentEvidenceQuery, want) {
			t.Errorf("runtime environment evidence query missing %q", want)
		}
	}
	for _, forbidden := range []string{"payload->>'environment_evidence' = candidate", "SELECT *"} {
		if strings.Contains(selectSupplyChainImpactRuntimeEnvironmentEvidenceQuery, forbidden) {
			t.Errorf("runtime environment evidence query contains forbidden %q", forbidden)
		}
	}
}

func TestListSupplyChainImpactRuntimeEnvironmentEvidenceFailsLoud(t *testing.T) {
	t.Parallel()

	store := PostgresSupplyChainImpactFindingStore{}
	_, err := store.ListSupplyChainImpactRuntimeEnvironmentEvidence(
		context.Background(),
		[]SupplyChainRuntimeEnvironmentCandidate{{SubjectDigest: "sha256:subject", Environment: "prod"}},
		nil,
		nil,
	)
	if err == nil || err.Error() != "supply chain runtime environment evidence database is required" {
		t.Fatalf("nil-DB error = %v, want fail-loud database error", err)
	}

	tooMany := make([]SupplyChainRuntimeEnvironmentCandidate, maxSupplyChainRuntimeEnvironmentCandidates+1)
	_, err = store.ListSupplyChainImpactRuntimeEnvironmentEvidence(context.Background(), tooMany, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "exceed limit 200") {
		t.Fatalf("oversized candidate error = %v, want limit error", err)
	}
}
