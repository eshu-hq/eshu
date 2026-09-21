// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Regression for #6162: an endpoint scanned in a different scope must not
// resolve, because GCPCloudResourceEdgeWriter stamps rel.scope_id and
// rel.generation_id from the EXECUTING intent, never from the row. A row built
// against another scope's resource is therefore written under this intent's
// scope, which is silent cross-scope misattribution.
//
// Observed in Ifá run 35598054762 (fault-injection shard 4/4, cell
// restartbackend): acme-demo-gcp-00 lost all 63 of its edges while 34 edges
// sourced from its resources appeared under acme-demo-gcp-03 and 29 under
// supply-chain-demo-project, all carrying the executing intent's stamps. The
// 63-lost/63-misattributed symmetry is why every count-based assertion passed
// and only the graph digest moved.
//
// The join index type already documents this rule -- "an endpoint resolves only
// if that resource was scanned in the same scope (the trust-boundary rule)" --
// but nothing enforced it.
func TestExtractGCPRelationshipEdgeRowsRefusesCrossScopeEndpoint(t *testing.T) {
	t.Parallel()

	const (
		intentScope  = "gcp:project:acme-demo-gcp-03:seed:4580"
		foreignScope = "gcp:project:acme-demo-gcp-00:seed:4580"
		foreignVM    = "//compute.googleapis.com/projects/acme-demo-gcp-00/zones/z/instances/vm-foreign"
		localDisk    = "//compute.googleapis.com/projects/acme-demo-gcp-03/zones/z/disks/disk-local"
	)

	resources := []facts.Envelope{
		{ScopeID: foreignScope, FactKind: facts.GCPCloudResourceFactKind, Payload: map[string]any{
			"full_resource_name": foreignVM,
			"asset_type":         "compute.googleapis.com/Instance",
			"project_id":         "acme-demo-gcp-00",
			"location":           "z",
		}},
		{ScopeID: intentScope, FactKind: facts.GCPCloudResourceFactKind, Payload: map[string]any{
			"full_resource_name": localDisk,
			"asset_type":         "compute.googleapis.com/Disk",
			"project_id":         "acme-demo-gcp-03",
			"location":           "z",
		}},
	}

	rels := []facts.Envelope{
		{ScopeID: intentScope, FactKind: facts.GCPCloudRelationshipFactKind, Payload: map[string]any{
			"source_full_resource_name": foreignVM,
			"target_full_resource_name": localDisk,
			"relationship_type":         "INSTANCE_TO_DISK",
			"target_asset_type":         "compute.googleapis.com/Disk",
			"support_state":             "supported",
		}},
	}

	rows, tally, _, err := ExtractGCPRelationshipEdgeRows(resources, rels, intentScope)
	if err != nil {
		t.Fatalf("ExtractGCPRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0: a cross-scope source endpoint must not "+
			"produce a row, or the writer stamps it with %q", len(rows), intentScope)
	}
	if got := tally.byMode[gcpJoinModeCrossScopeEndpoint]; got != 1 {
		t.Fatalf("tally.byMode[%q] = %d, want 1: the refusal must be counted, "+
			"not silently dropped", gcpJoinModeCrossScopeEndpoint, got)
	}
	if got := tally.crossScopeEndpoint["compute.googleapis.com/Disk"]; got != 1 {
		t.Fatalf("tally.crossScopeEndpoint[Disk] = %d, want 1", got)
	}
}

// A same-scope endpoint must still resolve: the guard must not degrade the
// normal path into silent data loss, which would trade one silent defect for
// another.
func TestExtractGCPRelationshipEdgeRowsAdmitsSameScopeEndpoint(t *testing.T) {
	t.Parallel()

	const (
		intentScope = "gcp:project:acme-demo-gcp-03:seed:4580"
		vm          = "//compute.googleapis.com/projects/acme-demo-gcp-03/zones/z/instances/vm-local"
		disk        = "//compute.googleapis.com/projects/acme-demo-gcp-03/zones/z/disks/disk-local"
	)

	resources := []facts.Envelope{
		{ScopeID: intentScope, FactKind: facts.GCPCloudResourceFactKind, Payload: map[string]any{
			"full_resource_name": vm,
			"asset_type":         "compute.googleapis.com/Instance",
			"project_id":         "acme-demo-gcp-03",
			"location":           "z",
		}},
		{ScopeID: intentScope, FactKind: facts.GCPCloudResourceFactKind, Payload: map[string]any{
			"full_resource_name": disk,
			"asset_type":         "compute.googleapis.com/Disk",
			"project_id":         "acme-demo-gcp-03",
			"location":           "z",
		}},
	}
	rels := []facts.Envelope{
		{ScopeID: intentScope, FactKind: facts.GCPCloudRelationshipFactKind, Payload: map[string]any{
			"source_full_resource_name": vm,
			"target_full_resource_name": disk,
			"relationship_type":         "INSTANCE_TO_DISK",
			"target_asset_type":         "compute.googleapis.com/Disk",
			"support_state":             "supported",
		}},
	}

	rows, tally, _, err := ExtractGCPRelationshipEdgeRows(resources, rels, intentScope)
	if err != nil {
		t.Fatalf("ExtractGCPRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: a same-scope endpoint must still resolve", len(rows))
	}
	if got := tally.byMode[gcpJoinModeCrossScopeEndpoint]; got != 0 {
		t.Fatalf("tally.byMode[%q] = %d, want 0", gcpJoinModeCrossScopeEndpoint, got)
	}
}

// An empty intent scope keeps the pre-#6162 behaviour so test wiring and any
// caller that cannot supply a scope is not silently converted into a no-op
// guard that never fires.
func TestExtractGCPRelationshipEdgeRowsEmptyIntentScopeKeepsLegacyJoin(t *testing.T) {
	t.Parallel()

	const foreignVM = "//compute.googleapis.com/projects/other/zones/z/instances/vm-foreign"
	const localDisk = "//compute.googleapis.com/projects/p/zones/z/disks/disk-local"

	resources := []facts.Envelope{
		{ScopeID: "scope-a", FactKind: facts.GCPCloudResourceFactKind, Payload: map[string]any{
			"full_resource_name": foreignVM,
			"asset_type":         "compute.googleapis.com/Instance",
			"project_id":         "other", "location": "z",
		}},
		{ScopeID: "scope-b", FactKind: facts.GCPCloudResourceFactKind, Payload: map[string]any{
			"full_resource_name": localDisk,
			"asset_type":         "compute.googleapis.com/Disk",
			"project_id":         "p", "location": "z",
		}},
	}
	rels := []facts.Envelope{
		{ScopeID: "scope-b", FactKind: facts.GCPCloudRelationshipFactKind, Payload: map[string]any{
			"source_full_resource_name": foreignVM,
			"target_full_resource_name": localDisk,
			"relationship_type":         "INSTANCE_TO_DISK",
			"target_asset_type":         "compute.googleapis.com/Disk",
			"support_state":             "supported",
		}},
	}

	rows, _, _, err := ExtractGCPRelationshipEdgeRows(resources, rels, "")
	if err != nil {
		t.Fatalf("ExtractGCPRelationshipEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 with an empty intent scope", len(rows))
	}
}
