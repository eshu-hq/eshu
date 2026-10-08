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
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_ahead_a", "package.json", `{"name":"@acme/shared"}`, now)
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
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_ahead_b", "package.json", `{"name":"@acme/other"}`, now.Add(2*time.Minute))
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
