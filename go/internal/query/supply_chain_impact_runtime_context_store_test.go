// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"
)

func TestAddSupplyChainRuntimeContextFactWorkloadIdentity(t *testing.T) {
	t.Parallel()

	out := map[string]SupplyChainRuntimeContext{}
	addSupplyChainRuntimeContextFact(out, workloadIdentityFactKindQuery, "repository:r_217415d9", map[string]any{
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

func TestAddSupplyChainRuntimeContextFactServiceSkipsRejectedOutcome(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{"ambiguous", "rejected", "unresolved", "stale"} {
		outcome := outcome
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			out := map[string]SupplyChainRuntimeContext{}
			addSupplyChainRuntimeContextFact(out, serviceCatalogCorrelationFactKind, "scope", map[string]any{
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

	out := map[string]SupplyChainRuntimeContext{}
	addSupplyChainRuntimeContextFact(out, serviceCatalogCorrelationFactKind, "scope", map[string]any{
		"repository_id":   "repository:r_217415d9",
		"service_id":      "service:demo-db",
		"provenance_only": true,
	})
	if _, ok := out["repository:r_217415d9"]; ok {
		t.Error("provenance-only service evidence should not resolve context")
	}
}

func TestAddSupplyChainRuntimeContextFactServiceExactDerivedAndEmptyOutcome(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{"exact", "derived", ""} {
		outcome := outcome
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			out := map[string]SupplyChainRuntimeContext{}
			addSupplyChainRuntimeContextFact(out, serviceCatalogCorrelationFactKind, "scope", map[string]any{
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
	// entity_keys (verified against the gate Postgres), deployment_ids is the
	// fallback key.
	out := map[string]SupplyChainRuntimeContext{}
	addSupplyChainRuntimeContextFact(out, platformMaterializationFactKindQuery, "git-repository-scope:repository:r_217415d9", map[string]any{
		"entity_keys": []any{"deployment:deployable-config"},
	})
	ctx := out["repository:r_217415d9"]
	if len(ctx.DeploymentIDs) != 1 || ctx.DeploymentIDs[0] != "deployment:deployable-config" {
		t.Errorf("DeploymentIDs = %v, want [deployment:deployable-config] from entity_keys", ctx.DeploymentIDs)
	}

	out = map[string]SupplyChainRuntimeContext{}
	addSupplyChainRuntimeContextFact(out, platformMaterializationFactKindQuery, "scope", map[string]any{
		"repository_id":  "repository:r_217415d9",
		"deployment_ids": []any{"deployment:demo-db-prod", "deployment:demo-db-staging"},
	})
	ctx = out["repository:r_217415d9"]
	if len(ctx.DeploymentIDs) != 2 {
		t.Errorf("DeploymentIDs = %v, want 2 deployments from deployment_ids fallback", ctx.DeploymentIDs)
	}
}

func TestAddSupplyChainRuntimeContextFactCICDEnvironment(t *testing.T) {
	t.Parallel()

	out := map[string]SupplyChainRuntimeContext{}
	addSupplyChainRuntimeContextFact(out, cicdRunCorrelationFactKind, "scope", map[string]any{
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

	if got := supplyChainRuntimeContextRepositoryID(map[string]any{
		"repository_id": "oci-registry://registry.example.com/demo",
	}, ""); got != "" {
		t.Errorf("repositoryID = %q, want \"\" for OCI path", got)
	}
}

func TestSupplyChainRuntimeContextRepositoryIDFromGitScope(t *testing.T) {
	t.Parallel()

	if got := supplyChainRuntimeContextRepositoryID(map[string]any{}, "git-repository-scope:repository:r_217415d9"); got != "repository:r_217415d9" {
		t.Errorf("repositoryID = %q, want repository:r_217415d9", got)
	}
}

func TestAddSupplyChainRuntimeContextFactIgnoresUnanchoredFact(t *testing.T) {
	t.Parallel()

	out := map[string]SupplyChainRuntimeContext{}
	addSupplyChainRuntimeContextFact(out, workloadIdentityFactKindQuery, "scan-target-xyz", map[string]any{
		"entity_keys": []any{"workload:x"},
	})
	if len(out) != 0 {
		t.Errorf("out = %v, want no context for fact with no repository anchor", out)
	}
}

func TestAddSupplyChainRuntimeContextFactWorkloadIDPayloadAndPrefixFilter(t *testing.T) {
	t.Parallel()

	// Mirror the reducer (supplyChainWorkloadIDsFromPayload): payload
	// workload_id first, then entity_keys filtered to workload:-prefixed keys —
	// a non-workload entity key must never become runtime context.
	out := map[string]SupplyChainRuntimeContext{}
	addSupplyChainRuntimeContextFact(out, workloadIdentityFactKindQuery, "repository:r_217415d9", map[string]any{
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
	)
	if err == nil {
		t.Fatal("ListSupplyChainImpactRuntimeContext() error = nil, want nil-DB error")
	}
	if got := err.Error(); got != "supply chain impact runtime context database is required" {
		t.Errorf("error = %q, want %q", got, "supply chain impact runtime context database is required")
	}
}
