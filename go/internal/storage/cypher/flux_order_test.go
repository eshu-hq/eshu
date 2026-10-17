// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

// structuralFamilyMarkers maps each Phase G structural family to the source
// node labels that appear in every one of its statements (retracts anchor
// on the same source label as upserts). A statement matching none or more
// than one family is a test failure, never a silent skip.
var structuralFamilyMarkers = []struct {
	family  string
	markers []string
}{
	{"atlantis", []string{"AtlantisProject"}},
	{"flux", []string{"FluxKustomization", "FluxHelmRelease"}},
	{"gitlab", []string{"GitlabPipeline", "GitlabJob"}},
	{"helm", []string{"HelmTemplateValueUsage"}},
}

// TestStructuralEdgeFamilyOrder pins the Phase G family order
// (atlantis < flux < gitlab < helm) and the intra-flux retract-before-upsert
// sequence through the production buildStructuralEdgeStatements path, so a
// leaf move that reorders an append or a retract fails here.
func TestStructuralEdgeFamilyOrder(t *testing.T) {
	t.Parallel()

	mat := canonical.CanonicalMaterialization{
		GenerationID:    "gen-2",
		FirstGeneration: false,
		RepoPath:        "/repo",
		Entities: []canonical.EntityRow{
			atlantisProjectRowEntity("uid-network", "network", "/repo/atlantis.yaml", "network", ""),
			atlantisProjectRowEntity("uid-staging", "staging", "/repo/atlantis.yaml", "staging", "network"),
			fluxKustomizationRowEntity("uid-k", "/repo/apps.yaml", "flux-system", "GitRepository", "flux-system", "flux-system"),
			fluxSourceRowEntity("FluxGitRepository", "uid-gr", "flux-system", "flux-system", "/repo/sources.yaml"),
			fluxHelmReleaseRowEntity("uid-hr", "/repo/release.yaml", "flux-system", "HelmRepository", "charts", "flux-system", "", "", ""),
			fluxSourceRowEntity("FluxHelmRepository", "uid-hr-repo", "charts", "flux-system", "/repo/sources.yaml"),
			gitlabPipelineEntityRow("uid-pipeline", "/repo/.gitlab-ci.yml"),
			gitlabJobEntityRow("uid-build", "build", "/repo/.gitlab-ci.yml", ""),
			gitlabJobEntityRow("uid-test", "test", "/repo/.gitlab-ci.yml", "build"),
			{
				EntityID:   "def-image-repo",
				Label:      "HelmValueDefinition",
				EntityName: "image.repository",
				FilePath:   "/repo/charts/webapp/values.yaml",
			},
			{
				EntityID:   "use-image-repo",
				Label:      "HelmTemplateValueUsage",
				EntityName: "image.repository",
				FilePath:   "/repo/charts/webapp/templates/deployment.yaml",
			},
		},
	}

	writer := NewCanonicalNodeWriter(nil, 100, nil)
	stmts := writer.buildStructuralEdgeStatements(mat)
	if len(stmts) == 0 {
		t.Fatal("buildStructuralEdgeStatements() returned no statements for a four-family materialization")
	}

	familyAt := make([]int, len(stmts))
	for i, stmt := range stmts {
		matched := -1
		for f, family := range structuralFamilyMarkers {
			for _, marker := range family.markers {
				if strings.Contains(stmt.Cypher, marker) {
					if matched != -1 && matched != f {
						t.Fatalf("statement %d matches two families (markers of %q and %q): %s",
							i, structuralFamilyMarkers[matched].family, family.family, stmt.Cypher)
					}
					matched = f
				}
			}
		}
		if matched == -1 {
			t.Fatalf("statement %d matches no structural family: %s", i, stmt.Cypher)
		}
		familyAt[i] = matched
	}

	// Family order: every statement of family N precedes every statement of
	// family N+1, proving the four explicit appends kept their order.
	for f := 0; f+1 < len(structuralFamilyMarkers); f++ {
		lastPrev, firstNext := -1, -1
		for i, family := range familyAt {
			if family == f {
				lastPrev = i
			}
			if family == f+1 && firstNext == -1 {
				firstNext = i
			}
		}
		if lastPrev == -1 {
			t.Fatalf("no statements for family %q", structuralFamilyMarkers[f].family)
		}
		if firstNext == -1 {
			t.Fatalf("no statements for family %q", structuralFamilyMarkers[f+1].family)
		}
		if lastPrev > firstNext {
			t.Fatalf("family order violated: last %q statement at %d follows first %q statement at %d",
				structuralFamilyMarkers[f].family, lastPrev,
				structuralFamilyMarkers[f+1].family, firstNext)
		}
	}

	// Intra-flux: every flux retract precedes every flux upsert, and every
	// flux retract is Drain-marked for NornicDB autocommit routing (#4476).
	lastRetract, firstUpsert := -1, -1
	for i, stmt := range stmts {
		if familyAt[i] != 1 {
			continue
		}
		switch stmt.Operation {
		case OperationCanonicalRetract:
			lastRetract = i
			if !stmt.Drain {
				t.Fatalf("flux retract statement %d has Drain = false, want true", i)
			}
		case OperationCanonicalUpsert:
			if firstUpsert == -1 {
				firstUpsert = i
			}
		default:
			t.Fatalf("flux statement %d has unexpected operation %q", i, stmt.Operation)
		}
	}
	if lastRetract == -1 {
		t.Fatal("no flux retract statements: expected kustomization + helmrelease retracts")
	}
	if firstUpsert == -1 {
		t.Fatal("no flux upsert statements: expected resolved RECONCILES_FROM merges")
	}
	if lastRetract > firstUpsert {
		t.Fatalf("intra-flux order violated: last retract at %d follows first upsert at %d",
			lastRetract, firstUpsert)
	}
}
