// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/lib/pq"
)

// TestContentWriterReapsStaleLineKeyedDependencyIDForFivefivezerosevenFormats
// extends TestContentWriterReapsStaleLineKeyedDependencyIDOnSectionKeyedMigration
// (#5357's migration reap proof) to #5507's remaining formats. It is the same
// id-churn shape: a repo's pom.xml/Cargo.toml dependency was ingested under
// the pre-#5507 line-keyed content.CanonicalEntityID; this Write() call
// represents the first re-ingest after the fix lands, so its fresh set
// carries only the new content.CanonicalEntityIDWithMetadata id — the old
// line-keyed id is absent from this call entirely, exactly how a real
// re-sync observes it. This is a ONE-TIME identity migration: no schema or
// generation-epoch bump is required, because the reap this test exercises
// already anti-joins on entity_id per path regardless of why the id changed
// — the same generic mechanism #5357 relied on, itself the same mechanism
// #5329's line_number fix relied on before that. See
// docs/internal/agent-guide.md and this package's content_writer_reap.go for
// the completeness invariant reaping depends on.
//
// The maven row exercises the discriminator path specifically (classifier),
// proving the migration reap still works once a package-manager-specific
// discriminator is folded into the id, not only for the discriminator-less
// (section, name) formats.
func TestContentWriterReapsStaleLineKeyedDependencyIDForFivefivezerosevenFormats(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	const repoID = "repository:r_12345678"

	// Pre-#5507 line-keyed ids this repo's manifests were stored under before
	// the migration. Neither is part of this Write() call's fresh set — a
	// real re-sync never re-mints the old scheme.
	staleCargoID := content.CanonicalEntityID(repoID, "Cargo.toml", "Variable", "serde", 8)
	staleMavenID := content.CanonicalEntityID(repoID, "pom.xml", "Variable", "io.netty:netty-tcnative-boringssl-static", 10)

	// The fresh ids this Write() call actually upserts for the same logical
	// dependencies under the #5507 scheme.
	freshCargoMetadata := map[string]any{
		"section":         "dependencies",
		"config_kind":     "dependency",
		"package_manager": "cargo",
		"manifest_name":   "serde",
	}
	freshCargoID := content.CanonicalEntityIDWithMetadata(repoID, "Cargo.toml", "Variable", "serde", 8, freshCargoMetadata)

	freshMavenMetadata := map[string]any{
		"section":               "dependencies",
		"config_kind":           "dependency",
		"package_manager":       "maven",
		"dependency_classifier": "linux-x86_64",
	}
	freshMavenID := content.CanonicalEntityIDWithMetadata(
		repoID, "pom.xml", "Variable", "io.netty:netty-tcnative-boringssl-static", 10, freshMavenMetadata,
	)

	entities := []content.EntityRecord{
		{
			EntityID:   freshCargoID,
			Path:       "Cargo.toml",
			EntityType: "Variable",
			EntityName: "serde",
			StartLine:  8,
			Metadata:   freshCargoMetadata,
		},
		{
			EntityID:   freshMavenID,
			Path:       "pom.xml",
			EntityType: "Variable",
			EntityName: "io.netty:netty-tcnative-boringssl-static",
			StartLine:  10,
			Metadata:   freshMavenMetadata,
		},
	}

	mat := content.Materialization{
		RepoID:       repoID,
		ScopeID:      "test-scope",
		GenerationID: "test-gen",
		Entities:     entities,
	}

	if _, err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}

	reapQuery, reapArgs := findReapExec(t, db)
	if !strings.Contains(reapQuery, "DELETE FROM content_entities") {
		t.Fatalf("reap query = %q, want a DELETE FROM content_entities", reapQuery)
	}

	paths, ok := reapArgs[1].(pq.StringArray)
	if !ok {
		t.Fatalf("reap path arg type = %T, want pq.StringArray", reapArgs[1])
	}
	if len(paths) != 2 {
		t.Fatalf("reap paths = %v, want both Cargo.toml and pom.xml", []string(paths))
	}

	freshIDs, ok := reapArgs[2].(pq.StringArray)
	if !ok {
		t.Fatalf("reap fresh-id arg type = %T, want pq.StringArray", reapArgs[2])
	}

	mustContain(t, freshIDs, freshCargoID)
	mustContain(t, freshIDs, freshMavenID)

	// The stale line-keyed ids are NOT in the fresh set — proving that if a
	// row for either still exists in content_entities from before this
	// migration, this anti-join correctly reaps it (entity_id <> ALL(freshIDs)
	// is true for both).
	mustNotContain(t, freshIDs, staleCargoID)
	mustNotContain(t, freshIDs, staleMavenID)

	if freshCargoID == staleCargoID {
		t.Fatalf("fresh cargo id (%q) unexpectedly equals the stale line-keyed id; the migration did not change the identity", freshCargoID)
	}
	if freshMavenID == staleMavenID {
		t.Fatalf("fresh maven id (%q) unexpectedly equals the stale line-keyed id; the migration did not change the identity", freshMavenID)
	}
}
