// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/yaml"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestKustomizeDeployableOverlayB7FixtureExtendsBase guards the B-7 corpus
// fixture tests/fixtures/ecosystems/kustomize-deployable-overlay against
// drift for the #5445 EXTENDS_BASE edge, mirroring
// go/internal/relationships/kustomize_b7_fixture_test.go's real-parser-driven
// proof style for the pre-existing DEPLOYS_FROM case. It runs the actual YAML
// parser against the fixture's root kustomization.yaml (which declares
// "./base" as a local resources entry, per collectKustomizeBaseRefs'
// remote/.yaml-suffix filtering) and its base/kustomization.yaml, then proves
// kustomizeExtendsBaseEdgeStatements resolves the overlay -> base edge.
//
// The base is modeled as already-persisted (returned by the resolver, not in
// mat.Entities), the representative delta shape: only the overlay file
// changed this cycle, and the base's own uid/base_refs must still resolve
// correctly from a prior-cycle read, matching KustomizeOverlayResolver's own
// documented reason for existing.
func TestKustomizeDeployableOverlayB7FixtureExtendsBase(t *testing.T) {
	t.Parallel()

	fixtureRoot := filepath.Join("..", "..", "..", "..", "tests", "fixtures", "ecosystems", "kustomize-deployable-overlay")

	overlayRow := parseKustomizeOverlayFixtureRow(t, filepath.Join(fixtureRoot, "kustomization.yaml"))
	baseRow := parseKustomizeOverlayFixtureRow(t, filepath.Join(fixtureRoot, "base", "kustomization.yaml"))

	// NOTE: isRemoteKustomizeRef (go/internal/parser/yaml/kustomize_semantics.go)
	// only detects an explicit "scheme://" prefix, so the fixture's
	// go-getter-shorthand remote resource ("github.com/acme/deployable-source//k8s?ref=v1.4.0",
	// no "://") is ALSO misclassified as a local base alongside "./base" --
	// a pre-existing parser gap, not introduced by this fixture and out of
	// this issue's scope to fix. It is harmless to EXTENDS_BASE by
	// construction: kustomizeBaseDirectory resolves it to a directory no
	// sibling KustomizeOverlay occupies, so it drops as a dangling base (see
	// TestKustomizeExtendsBaseEdgeStatements_DanglingBaseDropsSilently) rather
	// than producing a wrong edge.
	baseRefs, ok := overlayRow["bases"].([]string)
	if !ok {
		t.Fatalf("fixture kustomization.yaml bases = %#v (wrong type), want a []string containing \"./base\"", overlayRow["bases"])
	}
	var hasLocalBase bool
	for _, ref := range baseRefs {
		if ref == "./base" {
			hasLocalBase = true
		}
	}
	if !hasLocalBase {
		t.Fatalf("fixture kustomization.yaml bases = %#v, want a row containing \"./base\" -- fixture drifted from the #5445 slice-3 EXTENDS_BASE proof", baseRefs)
	}

	w := &CanonicalNodeWriter{
		kustomizeOverlayResolver: &fakeKustomizeOverlayResolver{
			rows: map[string][]KustomizeOverlayRow{
				"kustomize-deployable-overlay": {
					{UID: "uid-base", Path: "base/kustomization.yaml"},
				},
			},
		},
	}
	mat := projector.CanonicalMaterialization{
		RepoID:       "kustomize-deployable-overlay",
		GenerationID: "gen-fixture",
		Entities: []projector.EntityRow{
			{
				Label:    "KustomizeOverlay",
				EntityID: "uid-overlay",
				FilePath: "kustomization.yaml",
				Metadata: overlayRow,
			},
		},
	}

	stmts := w.kustomizeExtendsBaseEdgeStatements(context.Background(), mat)

	var found bool
	for _, stmt := range stmts {
		if stmt.Operation != OperationCanonicalUpsert {
			continue
		}
		rows, ok := stmt.Parameters["rows"].([]map[string]any)
		if !ok {
			continue
		}
		for _, row := range rows {
			if row["source_uid"] == "uid-overlay" && row["target_uid"] == "uid-base" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("fixture did not resolve an EXTENDS_BASE uid-overlay -> uid-base edge; statements=%+v", stmts)
	}

	_ = baseRow // parsed for the fixture-drift guard the FailNow above already exercises
}

// parseKustomizeOverlayFixtureRow runs the real YAML parser against path and
// returns the single kustomize_overlays row it produces.
func parseKustomizeOverlayFixtureRow(t *testing.T, path string) map[string]any {
	t.Helper()
	payload, err := yaml.Parse(path, false, yaml.Options{})
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	overlays, ok := payload["kustomize_overlays"].([]map[string]any)
	if !ok || len(overlays) != 1 {
		t.Fatalf("%s kustomize_overlays = %#v, want exactly one row", path, payload["kustomize_overlays"])
	}
	return overlays[0]
}
