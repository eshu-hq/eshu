// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"testing"
	"time"
)

// TestReducerContentionGateActiveCodeCallSymbolLoaderAheadManifestKeepsProducer
// reproduces #7609: scope:ahead-b's stored manifest was renamed by a
// generation that wrote content but never activated (Ack refused), so the
// stored manifest is ahead of the active generation while the active file
// facts still publish the old name. Both scopes' active facts carry
// @acme/shared, so the anchored scan must cover both: dropping scope:ahead-b
// would let scope:ahead-a's definition resolve alone and bypass the
// ambiguity rule.
func TestReducerContentionGateActiveCodeCallSymbolLoaderAheadManifestKeepsProducer(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()

	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:ahead-a", "repository:r_ahead_a", "generation-ahead-a", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_ahead_a", "package.json", `{"name":"@acme/shared"}`, "generation-ahead-a", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-ahead-a", "scope:ahead-a", "generation-ahead-a", "index.js", "@acme/shared", "Thing", now.Add(2*time.Second))

	// scope:ahead-b: the active generation still publishes @acme/shared, but
	// the stored manifest was overwritten by the refused generation.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:ahead-b", "repository:r_ahead_b", "generation-ahead-b", now)
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ('generation-ahead-b-refused', 'scope:ahead-b', 'snapshot', $1, $1, 'pending')`, now.Add(time.Minute)); err != nil {
		t.Fatalf("insert refused generation: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE scope_generations SET activated_at = $1
WHERE generation_id = 'generation-ahead-b'`, now.Add(time.Minute)); err != nil {
		t.Fatalf("stamp active generation activation: %v", err)
	}
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_ahead_b", "package.json", `{"name":"@acme/other"}`, "generation-ahead-b-refused", now.Add(2*time.Minute))
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-ahead-b", "scope:ahead-b", "generation-ahead-b", "index.js", "@acme/shared", "Thing", now.Add(3*time.Second))

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{
		"package:@acme/shared#Thing",
	})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-ahead-a", "fact-ahead-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v (the ahead manifest must not drop an active producer)", got, want)
	}
	if got, want := queryer.args[1][4], []string{"scope:ahead-a", "scope:ahead-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v", got, want)
	}
}

// TestReducerContentionGateActiveCodeCallSymbolLoaderSupersedeThenDeltaHole pins
// the supersede-then-delta shape (#7609, #7760): G3 wrote C3 (@acme/other)
// and was refused, then G4 (a delta that never touched package.json)
// activated and Ack superseded G3. The stored manifest is tagged with G3,
// whose activated_at is NULL, so the tag rule reads it dirty
// (unactivated_tag) and keeps the scope in the producer set; a status or
// timestamp comparison alone would miss, because G3 is superseded and C3
// predates G4's activation.
func TestReducerContentionGateActiveCodeCallSymbolLoaderSupersedeThenDeltaHole(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()

	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:hole-clean", "repository:r_hole_clean", "generation-hole-clean", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_hole_clean", "package.json", `{"name":"@acme/shared"}`, "generation-hole-clean", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-hole-clean", "scope:hole-clean", "generation-hole-clean", "index.js", "@acme/shared", "Thing", now.Add(2*time.Second))

	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope:hole', 'repository', 'git', 'repository:r_hole', 'git', 'scope:hole', $1, $1, 'active', 'generation-hole-g4')`, now); err != nil {
		t.Fatalf("insert scope:hole: %v", err)
	}
	for _, generation := range []struct {
		id          string
		status      string
		isDelta     bool
		observedAt  time.Time
		activatedAt *time.Time
	}{
		{"generation-hole-g1", "superseded", false, now.Add(-time.Hour), &[]time.Time{now.Add(-59 * time.Minute)}[0]},
		{"generation-hole-g3", "superseded", false, now.Add(-time.Minute), nil},
		{"generation-hole-g4", "active", true, now, &[]time.Time{now.Add(time.Minute)}[0]},
	} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, is_delta, activated_at
) VALUES ($1, 'scope:hole', 'snapshot', $2, $2, $3, $4, $5)`,
			generation.id, generation.observedAt, generation.status, generation.isDelta, generation.activatedAt); err != nil {
			t.Fatalf("insert generation %q: %v", generation.id, err)
		}
	}
	// C3 predates G4's activation: the timestamp leg alone would miss.
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_hole", "package.json", `{"name":"@acme/other"}`, "generation-hole-g3", now.Add(-30*time.Second))
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-hole", "scope:hole", "generation-hole-g4", "index.js", "@acme/shared", "Thing", now.Add(3*time.Second))

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{
		"package:@acme/shared#Thing",
	})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-hole-clean", "fact-hole"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v (the superseded refused generation must keep the scope dirty)", got, want)
	}
	if got, want := queryer.args[1][4], []string{"scope:hole", "scope:hole-clean"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v", got, want)
	}
}

// TestReducerContentionGateActiveCodeCallSymbolLoaderDirtyNonProducerStaysGated
// proves a dirty scope that publishes nothing relevant is scanned but cannot
// break single-producer resolution: the anchored definition match still gates
// on each definition's own package_id.
func TestReducerContentionGateActiveCodeCallSymbolLoaderDirtyNonProducerStaysGated(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()

	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:solo", "repository:r_solo", "generation-solo", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_solo", "package.json", `{"name":"@acme/solo"}`, "generation-solo", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-solo", "scope:solo", "generation-solo", "index.js", "@acme/solo", "run", now.Add(2*time.Second))

	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:noisy", "repository:r_noisy", "generation-noisy", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_noisy", "package.json", `{"name":"@acme/unrelated"}`, "generation-noisy-pending", now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-noisy", "scope:noisy", "generation-noisy", "index.js", "@acme/unrelated", "run", now.Add(3*time.Second))
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES ('generation-noisy-pending', 'scope:noisy', 'snapshot', $1, $1, 'pending')`, now.Add(time.Minute)); err != nil {
		t.Fatalf("insert pending generation: %v", err)
	}

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{
		"package:@acme/solo#run",
	})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := factIDs(loaded), []string{"fact-solo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v", got, want)
	}
	if got, want := queryer.args[1][4], []string{"scope:noisy", "scope:solo"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored scan producer scopes ($5) = %#v, want %#v (the dirty non-producer is scanned, then gated out)", got, want)
	}
}
