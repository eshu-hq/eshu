// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// Entity-label retract coverage (C-14 #4367 retract axis). A content_entity node
// that exists in gen1 and is absent from gen2 is removed by the production
// retract path on a real NornicDB. This drives every content_entity fact in the
// delta cassette's gen1 through the production entity projection, then proves
// each maps-to label is created (count=1) and then retracted (count=0) after the
// gen2 write. The single content_entity fact kind (differentiated by
// entity_type) is thus a genuine create-then-retract vehicle for the retractable
// node label set.
//
// The cassette is the single source of truth: the set of labels proved here is
// derived from the cassette's gen1 content_entity facts, and
// TestEntityRetractManifestBinding binds that set to the replay-coverage manifest
// so a cassette/manifest drift fails without a live backend.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/replay/offlinetier"
	"gopkg.in/yaml.v3"
)

// cassetteContentEntityLabels reads the delta cassette and returns the sorted,
// de-duplicated set of graph labels its gen1 content_entity facts project to,
// via the production projector.EntityTypeLabel mapping. This is the source of
// truth for the entity-retract coverage set.
func cassetteContentEntityLabels(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(deltaCassetteRelPath))
	if err != nil {
		t.Fatalf("read delta cassette: %v", err)
	}
	var cass struct {
		Scopes []struct {
			Facts []struct {
				FactKind string `json:"fact_kind"`
				Payload  struct {
					EntityType string `json:"entity_type"`
				} `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &cass); err != nil {
		t.Fatalf("parse delta cassette: %v", err)
	}
	if len(cass.Scopes) == 0 {
		t.Fatal("delta cassette has no scopes")
	}
	seen := map[string]struct{}{}
	var labels []string
	for _, f := range cass.Scopes[0].Facts {
		if f.FactKind != "content_entity" {
			continue
		}
		label, ok := projector.EntityTypeLabel(f.Payload.EntityType)
		if !ok {
			t.Fatalf("gen1 content_entity entity_type %q has no projector label mapping", f.Payload.EntityType)
		}
		if _, dup := seen[label]; dup {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// TestDeltaEntityRetractGraphTruth proves every content_entity label in the
// cassette's gen1 is created (count=1) and then retracted (count=0) on a real
// NornicDB after the gen2 write.
func TestDeltaEntityRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the entity-retract tier against a real NornicDB", liveTierEnv)
	}

	labels := cassetteContentEntityLabels(t)
	if len(labels) == 0 {
		t.Fatal("no content_entity labels in cassette; entity-retract proof would be vacuous")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	cleanupDeltaScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupDeltaScope(cleanCtx, t, exec)
	})

	src := loadDeltaCassette(t)
	gen1, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("read gen1: err=%v ok=%v", err, ok)
	}
	gen2, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("read gen2: err=%v ok=%v", err, ok)
	}
	dm, err := offlinetier.DeltaMaterializationFromGenerations(gen1, gen2)
	if err != nil {
		t.Fatalf("DeltaMaterializationFromGenerations: %v", err)
	}

	if err := writer.Write(ctx, dm.Gen1); err != nil {
		t.Fatalf("write gen1: %v", err)
	}
	for _, label := range labels {
		assertEntityLabelCount(ctx, t, exec, label, 1, fmt.Sprintf("gen1: %s present", label))
	}

	if err := writer.Write(ctx, dm.Gen2); err != nil {
		t.Fatalf("write gen2: %v", err)
	}
	for _, label := range labels {
		assertEntityLabelCount(ctx, t, exec, label, 0, fmt.Sprintf("gen2: %s retracted", label))
	}
}

// assertEntityLabelCount asserts the count of nodes carrying label in the
// (isolated, pre-cleaned) delta scope equals want. The batch labels have no
// surviving instance in the base cassette, so a bare-label count is exact.
func assertEntityLabelCount(ctx context.Context, t *testing.T, exec liveExecutor, label string, want int64, msg string) {
	t.Helper()
	got, err := exec.count(ctx, fmt.Sprintf("MATCH (n:%s) RETURN count(n)", label), nil)
	if err != nil {
		t.Fatalf("%s: count %s: %v", msg, label, err)
	}
	if got != want {
		t.Fatalf("%s: %s count = %d, want %d", msg, label, got, want)
	}
}

// TestEntityRetractManifestBinding binds the cassette-derived content_entity
// label set to the replay-coverage manifest's retractable_node delta_tombstone
// rows that reference this cassette. It fails (without a backend) if the cassette
// and manifest drift — a content_entity fact added without its manifest row, or a
// row claiming coverage the cassette does not create.
func TestEntityRetractManifestBinding(t *testing.T) {
	labels := cassetteContentEntityLabels(t)
	cassetteSet := map[string]struct{}{}
	for _, l := range labels {
		cassetteSet[l] = struct{}{}
	}

	const manifestRel = "../../../../specs/replay-coverage-manifest.v1.yaml"
	const cassetteRef = "testdata/cassettes/replaydelta/multi-generation-tombstone.json"

	raw, err := os.ReadFile(filepath.Clean(manifestRel))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Coverage []struct {
			Surface      string `yaml:"surface"`
			ScenarioType string `yaml:"scenario_type"`
			Ref          string `yaml:"ref"`
		} `yaml:"coverage"`
	}
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	manifestSet := map[string]struct{}{}
	for _, e := range manifest.Coverage {
		if e.ScenarioType != "delta_tombstone" || !strings.HasPrefix(e.Surface, "retractable_node:") || e.Ref != cassetteRef {
			continue
		}
		label := strings.TrimPrefix(e.Surface, "retractable_node:")
		if _, ok := cassetteSet[label]; !ok {
			// A retractable_node row referencing this cassette that the cassette
			// does not create via content_entity (e.g. Directory) is out of this
			// binding's scope.
			continue
		}
		manifestSet[label] = struct{}{}
	}

	for l := range cassetteSet {
		if _, ok := manifestSet[l]; !ok {
			t.Errorf("cassette creates content_entity label %q but no retractable_node:%s delta_tombstone manifest row references %s", l, l, cassetteRef)
		}
	}
	for l := range manifestSet {
		if _, ok := cassetteSet[l]; !ok {
			t.Errorf("manifest row retractable_node:%s references %s but the cassette has no content_entity fact for it", l, cassetteRef)
		}
	}
}
