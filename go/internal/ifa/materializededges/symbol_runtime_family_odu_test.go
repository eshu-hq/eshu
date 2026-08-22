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
	if len(odu.Facts) != 9 {
		t.Fatalf("expected 9 facts (1 repository, 3 file, 3 content_entity, 2 shared_followup), got %d", len(odu.Facts))
	}
	kinds := make(map[string]int, len(odu.Facts))
	for _, fact := range odu.Facts {
		kinds[fact.FactKind]++
		if fact.SchemaVersion == "" {
			t.Errorf("fact %s (%s) declares no schema_version; this guard would be vacuous", fact.StableFactKey, fact.FactKind)
		}
	}
	want := map[string]int{"repository": 1, "file": 3, "content_entity": 3, "shared_followup": 2}
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

// TestSymbolRuntimeFamilyCassetteLineNumbersAgree pins the one field the
// cassette states twice. Each of the three functions appears both as a
// content_entity fact (start_line) and inside the server.go file fact's
// parsed_file_data.functions (line_number). Today they agree by construction —
// the same authoring pass wrote both — which is consistency between two
// artifacts sharing a derivation root, not correctness, and no gate compares
// them.
//
// It matters because content.CanonicalEntityID hashes the line number into the
// entity id (repoID, relativePath, entityType, entityName, lineNumber). Move a
// function in parsed_file_data without moving its content_entity twin and every
// derived uid changes, so HANDLES_ROUTE/RUNS_IN/INVOKES_CLOUD_ACTION silently
// bind to entity ids nothing else resolves — with all three families' exact-set
// assertions still green, because they compare against expected-edge files
// written from the same drifted fixture.
//
// end_line is deliberately NOT asserted: the two representations disagree there
// today (content_entity carries the declaration span, parsed_file_data the body
// span), and nothing derives identity from it.
func TestSymbolRuntimeFamilyCassetteLineNumbersAgree(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := ifa.LoadSymbolRuntimeFamilyOdu(ifa.SymbolRuntimeFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("ifa.LoadSymbolRuntimeFamilyOdu: %v", err)
	}

	entityLines := make(map[string]int)
	parsedLines := make(map[string]int)

	for _, fact := range odu.Facts {
		switch fact.FactKind {
		case "content_entity":
			name := anyToStringValue(fact.Payload["entity_name"])
			if name == "" {
				continue
			}
			line, ok := anyToIntValue(fact.Payload["start_line"])
			if !ok {
				t.Fatalf("content_entity %q carries no numeric start_line; this guard would be vacuous", name)
			}
			entityLines[name] = line
		case "file":
			if anyToStringValue(fact.Payload["relative_path"]) != ifa.SymbolRuntimeFamilyServerPath {
				continue
			}
			parsed, ok := fact.Payload["parsed_file_data"].(map[string]any)
			if !ok {
				t.Fatal("server.go file fact carries no parsed_file_data object")
			}
			functions, ok := parsed["functions"].([]any)
			if !ok {
				t.Fatal("server.go parsed_file_data carries no functions array")
			}
			for _, entry := range functions {
				fn, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				name := anyToStringValue(fn["name"])
				line, numeric := anyToIntValue(fn["line_number"])
				if name == "" || !numeric {
					t.Fatalf("parsed_file_data function %q carries no numeric line_number", name)
				}
				parsedLines[name] = line
			}
		}
	}

	if len(entityLines) != 3 || len(parsedLines) != 3 {
		t.Fatalf("expected 3 content_entity and 3 parsed_file_data functions, got %d and %d; "+
			"a collapsed extraction would make the comparison below pass vacuously",
			len(entityLines), len(parsedLines))
	}

	for name, entityLine := range entityLines {
		parsedLine, ok := parsedLines[name]
		if !ok {
			t.Errorf("content_entity %q has no parsed_file_data counterpart; the cassette describes a function only once", name)
			continue
		}
		if entityLine != parsedLine {
			t.Errorf("%s: content_entity start_line = %d but parsed_file_data line_number = %d; "+
				"content.CanonicalEntityID hashes this number, so the two representations now derive different entity ids",
				name, entityLine, parsedLine)
		}
	}
}

// anyToIntValue reads a JSON number that survived decoding as float64 (the
// encoding/json default) or as an int, and reports whether it was numeric at
// all — a missing key must fail the caller's guard rather than silently read 0,
// which is a plausible line number.
func anyToIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	default:
		return 0, false
	}
}
