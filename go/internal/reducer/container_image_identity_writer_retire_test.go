// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"
)

// containerImageIdentityRetireStatement is the FROZEN normalized text of the
// retire statement. The bounding test compares the production constant against
// this string in full, not by substring, so ANY widening of the DELETE — an
// appended `OR TRUE`, a dropped predicate, a different target table — fails the
// test. The `fencing_token <= $5` guard is frozen here too: cut it and a stalled
// worker's late retire deletes the rows a fresher worker just wrote, which is a
// worse failure than the stale row this retire exists to remove.
//
// A substring/`strings.Contains` check would NOT catch that: appending
// `OR TRUE` leaves every original fragment present while turning the statement
// into an unbounded `DELETE FROM fact_records`. That exact false green was found
// on the sibling #5837 change, which is why this domain freezes the whole
// statement instead.
//
// The statement is a bare DELETE. It deliberately does NOT re-stamp the keep-set
// — reducerFactBatchInsertQuery binds the same token on the INSERT, so a keep-set
// stamp here is a no-op that still rewrites every kept row; see
// containerImageIdentityRetireQuery, and
// TestContainerImageIdentityRetireDoesNotRewriteKeepSetRowsLive for the
// row-version proof.
const containerImageIdentityRetireStatement = "DELETE FROM fact_records " +
	"WHERE fact_kind = $1 " +
	"AND scope_id = $2 " +
	"AND generation_id = $3 " +
	"AND fact_id <> ALL($4::text[]) " +
	"AND fencing_token <= $5"

// normalizeReducerSQL collapses all runs of whitespace to single spaces and
// trims the ends, so the frozen statement above can stay readable while still
// comparing byte-for-byte against the indented production constant.
func normalizeReducerSQL(query string) string {
	return strings.Join(strings.Fields(query), " ")
}

// isContainerImageIdentityRetireStatement recognizes the retire by its DELETE
// rather than by the constant identifier, so a widened production statement is
// still recognized as "the retire" and fails the assertion that inspects it,
// instead of vanishing from the call list and turning a real regression into a
// confusing "no retire issued".
//
// The match is on the DELETE — which is what the retire IS — rather than on a
// preamble. A preamble match is exactly what broke when the stamping CTE was
// dropped, and a recognizer keyed on a fragment a rewrite can delete stops
// recognizing the statement instead of failing on it.
//
// The match is case-insensitive for the same reason it is keyed on the DELETE: a
// rewrite that lowercased the keyword would otherwise make the retire disappear
// from the call list, which is the exact "no retire issued" confusion this
// recognizer exists to prevent.
func isContainerImageIdentityRetireStatement(query string) bool {
	return strings.Contains(
		strings.ToUpper(normalizeReducerSQL(query)),
		"DELETE FROM FACT_RECORDS",
	)
}

// containerImageIdentityRetireCall returns the single retire statement issued by
// a write, failing when the writer issued none or more than one.
func containerImageIdentityRetireCall(
	t *testing.T,
	calls []fakeWorkloadIdentityExecCall,
) fakeWorkloadIdentityExecCall {
	t.Helper()
	var found []fakeWorkloadIdentityExecCall
	for _, call := range calls {
		if isContainerImageIdentityRetireStatement(call.query) {
			found = append(found, call)
		}
	}
	if len(found) != 1 {
		t.Fatalf("retire statements issued = %d, want exactly 1", len(found))
	}
	return found[0]
}

// containerImageIdentityInsertCalls drops the trailing retire from a write's
// exec calls, so insert-shape assertions keep measuring insert batching rather
// than total statement count.
func containerImageIdentityInsertCalls(
	calls []fakeWorkloadIdentityExecCall,
) []fakeWorkloadIdentityExecCall {
	inserts := make([]fakeWorkloadIdentityExecCall, 0, len(calls))
	for _, call := range calls {
		if isContainerImageIdentityRetireStatement(call.query) {
			continue
		}
		inserts = append(inserts, call)
	}
	return inserts
}

// containerImageIdentityDecisionForOutcome builds one canonical decision for an
// image reference classified as outcome. Both durably-written outcomes
// (exact_digest and tag_resolved) carry CanonicalWrites=1, so both survive
// containerImageIdentityCanonicalDecisions' filter.
func containerImageIdentityDecisionForOutcome(
	imageRef string,
	digest string,
	outcome ContainerImageIdentityOutcome,
) ContainerImageIdentityDecision {
	return ContainerImageIdentityDecision{
		ImageRef:         imageRef,
		Digest:           digest,
		RepositoryID:     "oci-registry://registry.example.com/team/api",
		Outcome:          outcome,
		Reason:           "test decision for " + string(outcome),
		CanonicalWrites:  1,
		IdentityStrength: "tag_observation_with_digest",
	}
}

// TestContainerImageIdentityWriterRetiresSupersededDecisionForSameImageRef is
// the regression for the stale-decision defect.
//
// The fact identity embeds `outcome` — both directly
// (containerImageIdentityIdentity) and through the stable fact key and
// canonical_id built from it — so a replay that CHANGES an image reference's
// classification writes a DIFFERENT fact_id. The bulk insert is
// ON CONFLICT (fact_id) DO UPDATE, so without a retire the superseded row stays
// live (is_tombstone = false, same active generation) and
// PostgresContainerImageIdentityStore.ListContainerImageIdentities — which has
// no DISTINCT ON, GROUP BY, or per-digest latest-wins — returns BOTH
// contradictory decisions for one image.
//
// container_image_identity is in the bootstrap reopen slice precisely because a
// replay is expected once the cross-scope OCI generation activates, so this is
// the ordinary path, not an exotic one.
func TestContainerImageIdentityWriterRetiresSupersededDecisionForSameImageRef(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "repo:team-api"
		generationID = "generation-git"
		imageRef     = "registry.example.com/team/api:prod"
	)
	evidenceAsOf := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	newWrite := func(outcome ContainerImageIdentityOutcome) ContainerImageIdentityWrite {
		return ContainerImageIdentityWrite{
			IntentID:     "intent-image-identity",
			ScopeID:      scopeID,
			GenerationID: generationID,
			SourceSystem: "git",
			EvidenceAsOf: evidenceAsOf,
			Decisions: []ContainerImageIdentityDecision{
				containerImageIdentityDecisionForOutcome(imageRef, testContainerDigest, outcome),
			},
		}
	}

	first := newWrite(ContainerImageIdentityTagResolved)
	corrected := newWrite(ContainerImageIdentityExactDigest)

	firstFactID := containerImageIdentityFactID(first, first.Decisions[0])
	correctedFactID := containerImageIdentityFactID(corrected, corrected.Decisions[0])
	if firstFactID == correctedFactID {
		t.Fatal("first and corrected fact IDs are equal; this test no longer covers the identity churn it guards")
	}

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{DB: db}
	if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), first); err != nil {
		t.Fatalf("first write error = %v, want nil", err)
	}

	db.execs = nil
	if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), corrected); err != nil {
		t.Fatalf("corrected write error = %v, want nil", err)
	}

	retire := containerImageIdentityRetireCall(t, db.execs)
	if len(retire.args) != 5 {
		t.Fatalf("retire args = %d, want 5", len(retire.args))
	}
	if got, want := retire.args[0], any(containerImageIdentityFactKind); got != want {
		t.Fatalf("retire fact_kind = %v, want %v", got, want)
	}
	if got, want := retire.args[1], any(scopeID); got != want {
		t.Fatalf("retire scope_id = %v, want %v", got, want)
	}
	if got, want := retire.args[2], any(generationID); got != want {
		t.Fatalf("retire generation_id = %v, want %v", got, want)
	}
	keep, ok := retire.args[3].([]string)
	if !ok {
		t.Fatalf("retire keep-set type = %T, want []string", retire.args[3])
	}
	if len(keep) != 1 || keep[0] != correctedFactID {
		t.Fatalf("retire keep-set = %v, want exactly [%s] (the superseded row must not survive)", keep, correctedFactID)
	}
	if keep[0] == firstFactID {
		t.Fatal("retire keep-set still contains the superseded fact ID; the stale decision would stay live")
	}
}

// TestContainerImageIdentityWriterRetiresEverythingWhenNothingIsCanonical
// covers the second, subtler half of the defect: only exact_digest and
// tag_resolved set CanonicalWrites=1
// (go/internal/reducer/container_image_identity_registry.go), so a replay that
// DEMOTES an image to ambiguous_tag, unresolved, or stale_tag produces no
// durable row at all. An identity-keyed upsert therefore writes nothing over
// the previously-canonical row, and only an empty keep-set clears it.
//
// This is the reachable production shape: the digest-only reference resolves to
// exact_digest while exactly one registry observation exists for the digest, and
// falls to ambiguous_tag once a second cross-scope OCI generation activates with
// another repository observing the same digest.
func TestContainerImageIdentityWriterRetiresEverythingWhenNothingIsCanonical(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{DB: db}

	result, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		EvidenceAsOf: time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
		Decisions: []ContainerImageIdentityDecision{
			{
				ImageRef:        "registry.example.com/team/api:prod",
				Outcome:         ContainerImageIdentityAmbiguousTag,
				Reason:          "artifact digest matched multiple registry repositories",
				CanonicalWrites: 0,
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}

	retire := containerImageIdentityRetireCall(t, db.execs)
	keep, ok := retire.args[3].([]string)
	if !ok {
		t.Fatalf("retire keep-set type = %T, want []string", retire.args[3])
	}
	if len(keep) != 0 {
		t.Fatalf("retire keep-set = %v, want empty (a fully demoted generation retires every prior decision)", keep)
	}
}

// TestContainerImageIdentityRetireQueryIsBoundedToItsOwnPartition proves the
// retire can only ever delete this domain's own decisions for the exact
// (fact_kind, scope_id, generation_id) the intent owns. A retire that leaked
// past any of those predicates would delete another scope's, another
// generation's, or another domain's durable facts.
//
// The comparison is FULL-TEXT against the frozen statement, not a set of
// substring checks. A fragment-based check passes even when the statement is
// widened into an unbounded delete, because the original fragments all remain
// present — see containerImageIdentityRetireStatement for the sibling-change
// false green that motivates this shape.
func TestContainerImageIdentityRetireQueryIsBoundedToItsOwnPartition(t *testing.T) {
	t.Parallel()

	if got := normalizeReducerSQL(containerImageIdentityRetireQuery); got != containerImageIdentityRetireStatement {
		t.Fatalf(
			"retire statement changed.\n got: %s\nwant: %s\n"+
				"Any edit to this DELETE must be re-proven bounded before the frozen text is updated.",
			got, containerImageIdentityRetireStatement,
		)
	}
}

// TestContainerImageIdentityWriterRetiresAfterInsert proves ordering: the
// retire runs AFTER the bulk insert.
//
// That buys two things, and it is worth being precise about which, because it
// does NOT buy atomicity: a failed insert leaves the previous generation's
// decisions in place rather than clearing them and then writing nothing (proven
// by TestContainerImageIdentityWriterDoesNotRetireWhenInsertFails), and no
// reader ever sees this scope generation with ZERO decisions, which
// retire-first would expose for the width of the insert. The window that IS
// open is the opposite one: the insert and the retire are separate autocommit
// statements, so between them the superseded decision and the corrected one are
// both durable and both active.
func TestContainerImageIdentityWriterRetiresAfterInsert(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{DB: db}
	if _, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		EvidenceAsOf: time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC),
		Decisions: []ContainerImageIdentityDecision{
			containerImageIdentityDecisionForOutcome(
				"registry.example.com/team/api:prod",
				testContainerDigest,
				ContainerImageIdentityExactDigest,
			),
		},
	}); err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}

	if len(db.execs) != 2 {
		t.Fatalf("exec calls = %d, want 2 (one batched insert, one retire)", len(db.execs))
	}
	if db.execs[0].query != reducerFactBatchInsertQuery {
		t.Fatalf("first statement = %q, want the batched fact insert", db.execs[0].query)
	}
	if got := normalizeReducerSQL(db.execs[1].query); got != containerImageIdentityRetireStatement {
		t.Fatalf("second statement = %q, want the retire", got)
	}
}
