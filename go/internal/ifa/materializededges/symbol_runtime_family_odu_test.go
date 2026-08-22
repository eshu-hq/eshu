// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestSymbolRuntimeFamilyCassetteMatchesCompiledCatalog pins the compiled,
// binary-portable Odù (ifa.SymbolRuntimeFamilyOdu, what catalog_seed.go
// registers) to the committed cassette's strict projection, so a one-sided
// edit to either fails this focused suite -- mirroring
// TestDeployableUnitFamilyCassetteMatchesCompiledCatalog's role for its own
// family.
func TestSymbolRuntimeFamilyCassetteMatchesCompiledCatalog(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	compiled := ifa.SymbolRuntimeFamilyOdu().Odu
	fromCassette, err := ifa.LoadSymbolRuntimeFamilyOdu(ifa.SymbolRuntimeFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("ifa.LoadSymbolRuntimeFamilyOdu: %v", err)
	}
	if !reflect.DeepEqual(compiled, fromCassette) {
		t.Fatalf("compiled catalog Odù drifted from strict cassette projection\ncompiled: %#v\ncassette: %#v", compiled, fromCassette)
	}
}

// TestSymbolRuntimeFamilyCassetteDrivesAllThreeExpectedSets is the offline
// vacuity guard for #5995/#6000/#5997: the production
// extraction/workload-projection seams, over the ONE committed cassette,
// reproduce all three hand-derived expected sets EXACTLY. This is
// deliberately NOT called coverage -- it proves the extractors, not the live
// gate: the graph write is a MATCH-MATCH-MERGE on real backend node
// identities, which this offline pass never touches.
func TestSymbolRuntimeFamilyCassetteDrivesAllThreeExpectedSets(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := ifa.LoadSymbolRuntimeFamilyOdu(ifa.SymbolRuntimeFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("ifa.LoadSymbolRuntimeFamilyOdu: %v", err)
	}

	cases := []struct {
		name    string
		resolve func(ifa.Odu, string) (bool, string)
		path    string
	}{
		{"handles_route", resolveHandlesRouteMaterializedEdges, handlesRouteExpectedEdgesPath(repoRoot)},
		{"runs_in", resolveRunsInMaterializedEdges, runsInExpectedEdgesPath(repoRoot)},
		{"invokes_cloud_action", resolveInvokesCloudActionMaterializedEdges, invokesCloudActionExpectedEdgesPath(repoRoot)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, detail := tc.resolve(odu, tc.path)
			if !ok {
				t.Fatalf("%s: resolve(cassette odù) = (false, %q), want (true, ...)", tc.name, detail)
			}
			t.Log(detail)
		})
	}
}

// TestSymbolRuntimeFamilyCoversAllRegistryTypes stops each fixture degrading
// into a vacuous proof: each family owns exactly one edge type today, and an
// expected set that quietly dropped it would still parse as valid JSON with
// zero edges asserted for the family.
func TestSymbolRuntimeFamilyCoversAllRegistryTypes(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	cases := []struct {
		family string
		path   string
	}{
		{handlesRouteFamily, handlesRouteExpectedEdgesPath(repoRoot)},
		{runsInFamily, runsInExpectedEdgesPath(repoRoot)},
		{invokesCloudActionFamily, invokesCloudActionExpectedEdgesPath(repoRoot)},
	}
	for _, tc := range cases {
		t.Run(tc.family, func(t *testing.T) {
			expectedEdges, err := LoadExpectedEdges(tc.path, tc.family)
			if err != nil {
				t.Fatalf("LoadExpectedEdges: %v", err)
			}
			registry, err := MaterializedEdgeDomainEdgeTypes(tc.family)
			if err != nil {
				t.Fatalf("MaterializedEdgeDomainEdgeTypes(%s): %v", tc.family, err)
			}
			if missing := missingSymbolRuntimeExpectedTypes(expectedEdges, registry); len(missing) > 0 {
				t.Errorf("the expected-edge set exercises no %v edge; the family registers them, so the fixture proves exhaustiveness over less than the family owns", missing)
			}
		})
	}
}

// TestSymbolRuntimeFamilyOduPreservesEnvelopeFields stops the loader being
// more permissive than production, mirroring
// TestDeployableUnitFamilyOduPreservesEnvelopeFields.
func TestSymbolRuntimeFamilyOduPreservesEnvelopeFields(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := ifa.LoadSymbolRuntimeFamilyOdu(ifa.SymbolRuntimeFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("ifa.LoadSymbolRuntimeFamilyOdu: %v", err)
	}
	if len(odu.Facts) != 8 {
		t.Fatalf("expected 8 facts (1 repository, 2 file, 3 content_entity, 2 shared_followup), got %d", len(odu.Facts))
	}
	kinds := make(map[string]int, len(odu.Facts))
	for _, fact := range odu.Facts {
		kinds[fact.FactKind]++
		if fact.SchemaVersion == "" {
			t.Errorf("fact %s (%s) declares no schema_version; this guard would be vacuous", fact.StableFactKey, fact.FactKind)
		}
	}
	want := map[string]int{"repository": 1, "file": 2, "content_entity": 3, "shared_followup": 2}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("fact-kind breakdown = %v, want %v", kinds, want)
	}
}

// TestSymbolRuntimeFamilyCanonicalEntityIDLiterals independently reproduces
// (rather than trusts hand-typed) the three Function uid literals pinned in
// symbol_runtime_family_odu.go, mirroring
// TestShellExecCanonicalEntityIDLiterals.
func TestSymbolRuntimeFamilyCanonicalEntityIDLiterals(t *testing.T) {
	t.Parallel()

	got := content.CanonicalEntityID(
		ifa.SymbolRuntimeFamilyRepoID,
		ifa.SymbolRuntimeFamilyServerPath,
		"Function",
		ifa.SymbolRuntimeFamilyHandlerFunctionName,
		ifa.SymbolRuntimeFamilyHandlerFunctionLine,
	)
	if got != ifa.SymbolRuntimeFamilyHandlerFunctionUID {
		t.Errorf("HandleWidgets canonical uid = %q, want %q", got, ifa.SymbolRuntimeFamilyHandlerFunctionUID)
	}

	got = content.CanonicalEntityID(
		ifa.SymbolRuntimeFamilyRepoID,
		ifa.SymbolRuntimeFamilyServerPath,
		"Function",
		ifa.SymbolRuntimeFamilyHealthFunctionName,
		ifa.SymbolRuntimeFamilyHealthFunctionLine,
	)
	if got != ifa.SymbolRuntimeFamilyHealthFunctionUID {
		t.Errorf("HandleHealth canonical uid = %q, want %q", got, ifa.SymbolRuntimeFamilyHealthFunctionUID)
	}

	got = content.CanonicalEntityID(
		ifa.SymbolRuntimeFamilyRepoID,
		ifa.SymbolRuntimeFamilyServerPath,
		"Function",
		ifa.SymbolRuntimeFamilyCallerFunctionName,
		ifa.SymbolRuntimeFamilyCallerFunctionLine,
	)
	if got != ifa.SymbolRuntimeFamilyCallerFunctionUID {
		t.Errorf("InvokeAWS canonical uid = %q, want %q", got, ifa.SymbolRuntimeFamilyCallerFunctionUID)
	}
}

// TestSymbolRuntimeFamilyWorkloadAndEndpointIDLiterals independently
// reproduces the Workload id (a deterministic, non-hashed string) and the
// Endpoint id (a sha256-derived hash) pinned in symbol_runtime_family_odu.go
// and in the HANDLES_ROUTE/RUNS_IN expected-edge fixtures, by running the
// SAME reducer.ExtractWorkloadCandidates -> reducer.BuildProjectionRows seam
// the guards themselves run, over the compiled Odù's own facts.
func TestSymbolRuntimeFamilyWorkloadAndEndpointIDLiterals(t *testing.T) {
	t.Parallel()

	odu := ifa.SymbolRuntimeFamilyOdu().Odu
	candidates, deploymentEnvironments := reducer.ExtractWorkloadCandidates(odu.Facts)
	projection := reducer.BuildProjectionRows(candidates, deploymentEnvironments)

	if len(projection.WorkloadRows) != 1 {
		t.Fatalf("expected exactly 1 WorkloadRow, got %d: %+v", len(projection.WorkloadRows), projection.WorkloadRows)
	}
	if got := projection.WorkloadRows[0].WorkloadID; got != ifa.SymbolRuntimeFamilyWorkloadID {
		t.Errorf("WorkloadID = %q, want %q", got, ifa.SymbolRuntimeFamilyWorkloadID)
	}

	if len(projection.EndpointRows) != 2 {
		t.Fatalf("expected exactly 2 APIEndpointRows (one per distinct path), got %d: %+v", len(projection.EndpointRows), projection.EndpointRows)
	}
	wantEndpointIDs := map[string]string{
		"/widgets": ifa.SymbolRuntimeFamilyEndpointID,
		"/healthz": ifa.SymbolRuntimeFamilyHealthEndpointID,
	}
	for _, row := range projection.EndpointRows {
		want, ok := wantEndpointIDs[row.Path]
		if !ok {
			t.Fatalf("unexpected endpoint path %q in projection: %+v", row.Path, row)
		}
		if row.EndpointID != want {
			t.Errorf("EndpointID for path %q = %q, want %q", row.Path, row.EndpointID, want)
		}
	}
}

// TestSymbolRuntimeFamilyRepositoryIdentityDoesNotCollideWithSiblings mirrors
// TestDeployableUnitFamilyRepositoryIdentityDoesNotCollideWithSiblings: a
// shared local_path lets canonical path cleanup delete a sibling family's
// repository, and a shared generation ID violates the durable
// active-generation uniqueness constraint when both scopes ACK.
func TestSymbolRuntimeFamilyRepositoryIdentityDoesNotCollideWithSiblings(t *testing.T) {
	t.Parallel()
	odu := ifa.SymbolRuntimeFamilyOdu().Odu

	var localPath, generationID string
	for _, fact := range odu.Facts {
		if fact.FactKind == "repository" {
			localPath = anyToStringValue(fact.Payload["local_path"])
		}
	}
	if odu.Facts[0].GenerationID != "" {
		generationID = odu.Facts[0].GenerationID
	}
	if localPath == "" {
		t.Fatal("symbol-runtime repository fact has no local_path; canonical file and entity paths would be unanchored")
	}
	if generationID == "" {
		t.Fatal("symbol-runtime Odù has no generation ID; active-generation publication would be unidentifiable")
	}
	for _, sibling := range []string{ifa.SQLFamilyLocalPath, ifa.CodeCallFamilyLocalPath, ifa.ShellExecFamilyLocalPath} {
		if localPath == sibling {
			t.Fatalf("symbol-runtime local_path %q collides with a sibling family's repository path", localPath)
		}
	}
	for _, sibling := range []string{ifa.SQLFamilyGenerationID, ifa.CodeCallFamilyGenerationID} {
		if generationID == sibling {
			t.Fatalf("symbol-runtime generation ID %q collides with a sibling family's generation", generationID)
		}
	}
}
