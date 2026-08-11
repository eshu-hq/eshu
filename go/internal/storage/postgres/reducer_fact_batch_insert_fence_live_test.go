// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Real-Postgres proofs for reducerFactBatchInsertQuery's ON CONFLICT semantics
// (#5847).
//
// The statement is shared by seven reducer writers, only one of which
// (container_image_identity) opts into a fencing token. Its conflict behavior
// therefore has to be correct for the fenced caller AND inert for the six that
// leave reducerFactRow.FencingToken at zero. Neither property can be asserted
// against a fake execer: both are properties of the SQL, not of the arguments.

// containerImageIdentityEnrichedWrite builds a fenced one-decision write whose
// IDENTITY (scope, generation, image_ref, outcome) is fixed while its PAYLOAD
// varies with sourceRevision.
//
// That split is the whole point. containerImageIdentityIdentity embeds only
// scope_id, generation_id, image_ref and outcome, so two passes that agree on
// the outcome mint the SAME fact_id and collide on the insert's conflict key —
// while source_revision, source_revision_provenance,
// build_provenance_repository_ids and evidence_fact_ids are all payload-only and
// are filled in by the cross-scope enrichment
// (applyCIRunDigestRevision/applySLSADigestRevision,
// container_image_identity_registry.go) whose visibility depends on which
// generations are active at load time. A pass that runs before the CI/SLSA
// generation activates resolves the same outcome with a POORER payload than a
// pass that runs after it, which is precisely why this domain sits in the
// bootstrap reopen slice.
func containerImageIdentityEnrichedWrite(
	scopeID, generationID, suffix string,
	evidenceAsOf time.Time,
	sourceRevision string,
) reducer.ContainerImageIdentityWrite {
	write := containerImageIdentityLiveWrite(
		scopeID, generationID, suffix, evidenceAsOf, reducer.ContainerImageIdentityExactDigest,
	)
	write.ClaimEpoch = 1
	write.Decisions[0].SourceRevision = sourceRevision
	if sourceRevision != "" {
		write.Decisions[0].SourceRevisionProvenance = "ci_run_commit"
		write.Decisions[0].BuildProvenanceRepositoryIDs = []string{"repository:r_builder"}
	}
	return write
}

// containerImageIdentityStoredPayload reads the payload fields and token of the
// single identity row in a partition.
func containerImageIdentityStoredPayload(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID string,
) (sourceRevision string, provenance string, fencingToken int64) {
	t.Helper()

	if err := db.QueryRowContext(
		ctx,
		`SELECT COALESCE(payload->>'source_revision', ''),
		        COALESCE(payload->>'source_revision_provenance', ''),
		        fencing_token
		 FROM fact_records
		 WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = $3`,
		scopeID, generationID, containerImageIdentityLiveFactKind,
	).Scan(&sourceRevision, &provenance, &fencingToken); err != nil {
		t.Fatalf("read stored identity payload for %s/%s: %v", scopeID, generationID, err)
	}
	return sourceRevision, provenance, fencingToken
}

func containerImageIdentityFenceLiveWriter(
	db *sql.DB,
) reducer.PostgresContainerImageIdentityWriter {
	adapter := SQLDB{DB: db}
	cutoverStore := NewContainerImageIdentityCutoverStore(adapter)
	return reducer.PostgresContainerImageIdentityWriter{
		DB:                  adapter,
		Beginner:            ContainerImageIdentityBeginner{Beginner: adapter},
		CutoverLookup:       cutoverStore,
		LegacyCleanupLookup: cutoverStore,
		ClaimedExecer:       ContainerImageIdentityClaimedExecer{DB: adapter},
	}
}

func seedContainerImageIdentityFenceLiveClaim(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	write reducer.ContainerImageIdentityWrite,
) {
	t.Helper()
	seedContainerImageIdentityAckWorkItem(
		t,
		ctx,
		db,
		write.IntentID,
		write.ScopeID,
		write.GenerationID,
		"reducer-"+write.IntentID,
		time.Now().UTC().Add(5*time.Minute),
		time.Now().UTC(),
	)
}

// TestReducerFactBatchInsertRejectsStaleContentUpsertLive is the regression for
// the hole a token-only conflict guard leaves open.
//
// Raising the token with GREATEST while assigning the content columns
// unconditionally protects the TOKEN and nothing else. A stalled worker
// re-upserting a fact_id that a fresher worker already wrote overwrites the
// payload with its own poorer view, and GREATEST then leaves the FRESHER token
// on it. The row ends up carrying stale content while advertising a fresh
// watermark. That is worse than an unfenced row, because the token now vouches
// for content it did not come from, and any consumer that ranks rows by it —
// the #5854 retire included — would trust the wrong one.
//
// The two writes here agree on the outcome, so they collide rather than
// producing two rows, and differ only in payload fields the cross-scope
// enrichment fills in. That is the reachable production shape: a worker that
// read its evidence before the CI/SLSA generation activated, stalled past its
// lease, and landed after a worker that read after activation.
//
// The guard on the conflict clause rejects the whole update — content included —
// rather than merging the two passes.
func TestReducerFactBatchInsertRejectsStaleContentUpsertLive(t *testing.T) {
	sqlDB, ctx := containerImageIdentityFenceLiveDB(t)

	suffix := fmt.Sprintf("5847-stale-content-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, scopeID, generationID)

	freshEvidenceAsOf := time.Now().UTC()
	staleEvidenceAsOf := freshEvidenceAsOf.Add(-5 * time.Minute)

	freshWrite := containerImageIdentityEnrichedWrite(
		scopeID, generationID, suffix, freshEvidenceAsOf, "commit-fresh",
	)
	seedContainerImageIdentityFenceLiveClaim(t, ctx, sqlDB, freshWrite)
	writer := containerImageIdentityFenceLiveWriter(sqlDB)

	// Worker B: read after the CI generation activated, so its decision carries
	// the build provenance. Lands first.
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, freshWrite); err != nil {
		t.Fatalf("fresh write error = %v, want nil", err)
	}

	// Worker A: the stalled holder. Same image, same outcome, so the SAME
	// fact_id — but read before activation, so no revision and no build
	// provenance.
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, containerImageIdentityEnrichedWrite(
		scopeID, generationID, suffix, staleEvidenceAsOf, "",
	)); err != nil {
		t.Fatalf("stale write error = %v, want nil", err)
	}

	revision, provenance, token := containerImageIdentityStoredPayload(t, ctx, sqlDB, scopeID, generationID)
	if want := freshEvidenceAsOf.UnixMicro(); token != want {
		t.Fatalf("stored fencing_token = %d, want %d (the fresher watermark)", token, want)
	}
	if revision != "commit-fresh" || provenance != "ci_run_commit" {
		t.Fatalf(
			"stored source_revision/provenance = %q/%q, want \"commit-fresh\"/\"ci_run_commit\": "+
				"a stalled worker overwrote a fresher row's CONTENT while the conflict guard kept the "+
				"fresher TOKEN on it, so the row advertises a freshness its payload does not have and "+
				"any consumer that ranks rows by that token trusts the wrong one",
			revision, provenance,
		)
	}
}

// TestReducerFactBatchInsertAppliesEqualTokenRetryLive pins the boundary the
// guard is written with `<=` rather than `<` for.
//
// A retry, a redelivery, or a second chunk of the SAME pass carries the SAME
// watermark, because the watermark is the evidence-read time and the pass read
// its evidence once. Under `<` every one of those would be silently discarded —
// the statement would report success while writing nothing — which is exactly
// the silent-failure shape this repository forbids. Under `<=` the retry
// applies.
func TestReducerFactBatchInsertAppliesEqualTokenRetryLive(t *testing.T) {
	sqlDB, ctx := containerImageIdentityFenceLiveDB(t)

	suffix := fmt.Sprintf("5847-equal-token-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, scopeID, generationID)

	evidenceAsOf := time.Now().UTC()
	firstWrite := containerImageIdentityEnrichedWrite(
		scopeID, generationID, suffix, evidenceAsOf, "",
	)
	seedContainerImageIdentityFenceLiveClaim(t, ctx, sqlDB, firstWrite)
	writer := containerImageIdentityFenceLiveWriter(sqlDB)

	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, firstWrite); err != nil {
		t.Fatalf("first write error = %v, want nil", err)
	}
	// Same watermark, richer payload: the retry of a pass that got further.
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, containerImageIdentityEnrichedWrite(
		scopeID, generationID, suffix, evidenceAsOf, "commit-retry",
	)); err != nil {
		t.Fatalf("retry write error = %v, want nil", err)
	}

	revision, _, token := containerImageIdentityStoredPayload(t, ctx, sqlDB, scopeID, generationID)
	if want := evidenceAsOf.UnixMicro(); token != want {
		t.Fatalf("stored fencing_token = %d, want %d", token, want)
	}
	if revision != "commit-retry" {
		t.Fatalf(
			"stored source_revision = %q, want \"commit-retry\": an equal-token retry must APPLY. "+
				"A `<` guard would drop it silently — the same pass reads its evidence once, so every "+
				"retry and every chunk of it carries the identical watermark",
			revision,
		)
	}
}

// TestReducerFactBatchInsertStaysInertForUnfencedWritersLive proves the conflict
// guard changes nothing for the six callers that never opted into a token.
//
// They leave reducerFactRow.FencingToken at zero and their rows sit at the
// column default zero, so the guard evaluates `0 <= 0` and the update proceeds
// exactly as it did before. That is a claim about SQL, not about arguments, and
// a shared statement with seven callers is precisely where an unverified
// inertness claim does damage: were the guard to reject these, a re-run would
// stop updating its own rows and report success while writing nothing.
//
// service_catalog_correlation is used as the representative: its identity is
// (scope, generation, provider, entity_ref), so changing only `reason` between
// two writes collides on the same fact_id with a different payload.
func TestReducerFactBatchInsertStaysInertForUnfencedWritersLive(t *testing.T) {
	sqlDB, ctx := containerImageIdentityFenceLiveDB(t)

	suffix := fmt.Sprintf("5847-unfenced-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, scopeID, generationID)

	writer := reducer.PostgresServiceCatalogCorrelationWriter{DB: sqlDB}
	newWrite := func(reason string) reducer.ServiceCatalogCorrelationWrite {
		return reducer.ServiceCatalogCorrelationWrite{
			IntentID:     "intent-" + suffix,
			ScopeID:      scopeID,
			GenerationID: generationID,
			SourceSystem: "git",
			Cause:        "unfenced inertness proof",
			Decisions: []reducer.ServiceCatalogCorrelationDecision{
				{
					Provider:  "backstage",
					EntityRef: "component:default/api",
					Outcome:   reducer.ServiceCatalogCorrelationExact,
					Reason:    reason,
				},
			},
		}
	}

	if _, err := writer.WriteServiceCatalogCorrelations(ctx, newWrite("first pass")); err != nil {
		t.Fatalf("first write error = %v, want nil", err)
	}
	if _, err := writer.WriteServiceCatalogCorrelations(ctx, newWrite("second pass")); err != nil {
		t.Fatalf("second write error = %v, want nil", err)
	}

	var reason string
	var token int64
	if err := sqlDB.QueryRowContext(
		ctx,
		`SELECT COALESCE(payload->>'reason', ''), fencing_token FROM fact_records
		 WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID,
	).Scan(&reason, &token); err != nil {
		t.Fatalf("read unfenced writer's row: %v", err)
	}
	if token != 0 {
		t.Fatalf("unfenced writer's fencing_token = %d, want 0 (it never opted in)", token)
	}
	if reason != "second pass" {
		t.Fatalf(
			"stored reason = %q, want \"second pass\": the conflict guard must be inert for a caller "+
				"that binds 0 against a row at 0, or every non-opted-in domain silently stops updating "+
				"its own rows while still reporting success",
			reason,
		)
	}
}
