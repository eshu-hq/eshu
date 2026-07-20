// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/parser/yaml"
)

// TestSnapshotEmitsFluxHelmReleaseAndRepositoryContentEntities is the issue
// #5483 C1 B-7 regression: the collector's snapshotEntityBuckets is a SECOND,
// independently hand-maintained bucket->label list (alongside
// content/shape's contentEntityBuckets). entityBucketsFromParsed walks ONLY
// snapshotEntityBuckets, so a parser bucket absent from it emits zero content
// entities -- no facts, no graph nodes -- even though the parser produces the
// bucket and the projector knows the label. Before this test the two new Flux
// Helm buckets were registered in the parser and content/shape but NOT in the
// collector list, so the golden-corpus gate's rc-153 / rn-flux-helm-* graph
// assertions all resolved count=0.
//
// This drives the REAL YAML parser over the SAME kubernetes_comprehensive
// fixtures the B-12 snapshot asserts against, then the REAL
// entityBucketsFromParsed + bucket->label mapping, and proves a
// FluxHelmRelease / FluxHelmRepository content-entity snapshot is emitted with
// its typed metadata -- the exact parse -> content-entity step the gate proved
// was broken.
func TestSnapshotEmitsFluxHelmReleaseAndRepositoryContentEntities(t *testing.T) {
	t.Parallel()

	fixtureDir, err := filepath.Abs(filepath.Join("..", "..", "..", "tests", "fixtures", "ecosystems", "kubernetes_comprehensive"))
	if err != nil {
		t.Fatalf("resolve fixture dir: %v", err)
	}

	cases := []struct {
		name       string
		file       string
		wantLabel  string
		wantName   string
		wantMetaKV [2]string // one typed metadata key/value the entity must carry
	}{
		{
			name:       "helmrelease",
			file:       "flux-helmrelease.yaml",
			wantLabel:  "FluxHelmRelease",
			wantName:   "podinfo",
			wantMetaKV: [2]string{"source_ref_kind", "HelmRepository"},
		},
		{
			name:       "helmrepository",
			file:       "flux-helmrepository.yaml",
			wantLabel:  "FluxHelmRepository",
			wantName:   "podinfo",
			wantMetaKV: [2]string{"url", "https://stefanprodan.github.io/podinfo"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload, err := yaml.Parse(filepath.Join(fixtureDir, tc.file), false, shared.Options{})
			if err != nil {
				t.Fatalf("yaml.Parse(%s) error = %v", tc.file, err)
			}

			snapshots := snapshotsFromParsedPayload(t, tc.file, payload)

			var found *ContentEntitySnapshot
			for i := range snapshots {
				if snapshots[i].EntityType == tc.wantLabel && snapshots[i].EntityName == tc.wantName {
					found = &snapshots[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("no %s content entity emitted from %s; the collector's snapshotEntityBuckets is missing the parser bucket (entityBucketsFromParsed silently drops it, so no graph node ever materializes). Got snapshots: %+v", tc.wantLabel, tc.file, snapshots)
			}

			key, want := tc.wantMetaKV[0], tc.wantMetaKV[1]
			got, _ := found.Metadata[key].(string)
			if got != want {
				t.Fatalf("%s metadata[%q] = %#v, want %#v (typed field must survive the snapshot path)", tc.wantLabel, key, found.Metadata[key], want)
			}
		})
	}
}
