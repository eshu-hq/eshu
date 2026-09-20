// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"context"
	"fmt"
	"sort"
	"testing"

	collectorsubmodule "github.com/eshu-hq/eshu/go/internal/collector/submodule"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestSubmodulePinFamilyCassetteSurvivesProductionFactOrder proves the
// checked-in cassette through cassette.Source, then orders its envelopes by
// FactStore.ListFactsByKind's durable (observed_at, fact_id) contract before
// invoking the production extractor. The offline family loader preserves JSON
// array order and cannot prove this boundary.
func TestSubmodulePinFamilyCassetteSurvivesProductionFactOrder(t *testing.T) {
	t.Parallel()

	envelopes := submodulePinCassetteFactsInProductionOrder(t)
	rows, quarantined, err := reducer.ExtractSubmodulePinEdgeRowsWithQuarantine(
		envelopes,
		"gen-ifa-submodule-pin-family-1",
	)
	if err != nil {
		t.Fatalf("ExtractSubmodulePinEdgeRowsWithQuarantine: %v", err)
	}
	if len(quarantined) > 0 {
		t.Fatalf("%d fact(s) quarantined", len(quarantined))
	}

	const wantSHA = "sha-libfoo-pin-a-current"
	for _, row := range rows {
		if row["parent_repo_id"] == ifa.SubmodulePinFamilyRepoID && row["submodule_path"] == "vendor/libfoo" {
			if got := row["pinned_sha"]; got != wantSHA {
				t.Fatalf("production-ordered vendor/libfoo pinned_sha = %q, want %q", got, wantSHA)
			}
			return
		}
	}
	t.Fatal("production-ordered cassette produced no vendor/libfoo row")
}

// TestSubmodulePinFamilyCassetteUsesOneProductionIdentityPerPin prevents a
// hand-authored cassette from assigning two stable fact identities to the
// same production (parent_repo_id, submodule_path) identity. The real
// collector emits exactly one stable key for that tuple; distinct keys survive
// persistence and make any last-seen winner an accidental fact_id ordering.
func TestSubmodulePinFamilyCassetteUsesOneProductionIdentityPerPin(t *testing.T) {
	t.Parallel()

	seen := make(map[string]string)
	var pinCount int
	for _, env := range submodulePinCassetteFactsInProductionOrder(t) {
		if env.FactKind != facts.SubmodulePinFactKind {
			continue
		}
		pinCount++
		pin, err := factschema.DecodeSubmodulePin(factschema.Envelope{
			FactKind: env.FactKind, SchemaVersion: env.SchemaVersion, Payload: env.Payload,
		})
		if err != nil {
			t.Fatalf("DecodeSubmodulePin(%s): %v", env.StableFactKey, err)
		}
		key := pin.ParentRepoID + "\x00" + pin.SubmodulePath
		emitted := collectorsubmodule.Emit(
			collectorsubmodule.FixtureContext{},
			pin.ParentRepoID,
			".gitmodules",
			fmt.Sprintf("[submodule \"fixture\"]\n\tpath = %s\n\turl = https://git.example.invalid/fixture.git\n", pin.SubmodulePath),
		)
		if len(emitted) != 1 {
			t.Fatalf("collector emitted %d facts for production pin identity (%q, %q), want 1", len(emitted), pin.ParentRepoID, pin.SubmodulePath)
		}
		wantStableKey := emitted[0].StableFactKey
		if env.StableFactKey != wantStableKey {
			t.Fatalf("production pin identity (%q, %q) stable key = %q, want collector-derived %q", pin.ParentRepoID, pin.SubmodulePath, env.StableFactKey, wantStableKey)
		}
		if prior, ok := seen[key]; ok && prior != env.StableFactKey {
			t.Fatalf("production pin identity (%q, %q) has distinct stable keys %q and %q", pin.ParentRepoID, pin.SubmodulePath, prior, env.StableFactKey)
		}
		seen[key] = env.StableFactKey
	}
	if pinCount == 0 {
		t.Fatal("cassette contains no submodule.pin facts")
	}
}

func submodulePinCassetteFactsInProductionOrder(t *testing.T) []facts.Envelope {
	t.Helper()
	repoRoot := repoRootDir(t)
	source, err := cassette.NewSource(ifa.SubmodulePinFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("cassette.Source.Next: %v", err)
	}
	if !ok {
		t.Fatal("cassette.Source.Next returned ok=false for the family cassette")
	}

	var envelopes []facts.Envelope
	for env := range collected.Facts {
		envelopes = append(envelopes, env)
	}
	sort.Slice(envelopes, func(i, j int) bool {
		if envelopes[i].ObservedAt.Equal(envelopes[j].ObservedAt) {
			return envelopes[i].FactID < envelopes[j].FactID
		}
		return envelopes[i].ObservedAt.Before(envelopes[j].ObservedAt)
	})
	return envelopes
}
