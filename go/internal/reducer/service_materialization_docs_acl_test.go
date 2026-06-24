// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDocsEvidencePayloadCarriesObservedSourceACLState proves the bounded
// source_acl_state is projected verbatim into the docs evidence read-model
// payload for every collector-observed state, alongside (never folded into) the
// freshness-adjacent observable fields. ACL state and freshness are distinct
// truth axes (#2138), so the projection keeps source_acl_state as its own field.
func TestDocsEvidencePayloadCarriesObservedSourceACLState(t *testing.T) {
	t.Parallel()

	states := []string{
		facts.SourceACLStateAllowed,
		facts.SourceACLStateDenied,
		facts.SourceACLStatePartial,
		facts.SourceACLStateMissing,
		facts.SourceACLStateStale,
	}
	for _, state := range states {
		state := state
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			payload := serviceDocumentationEvidencePayload(ServiceDocumentationRecord{
				SourceSystem:   "confluence",
				SourceRecordID: "section:deploy",
				DocumentID:     "doc:runbook",
				FactKind:       facts.DocumentationEntityMentionFactKind,
				SourceACLState: state,
			})
			got, ok := payload["source_acl_state"]
			if !ok {
				t.Fatalf("docs payload must carry observed source_acl_state %q: %#v", state, payload)
			}
			if got != state {
				t.Fatalf("docs payload source_acl_state = %#v, want %q (verbatim, no upgrade)", got, state)
			}
		})
	}
}

// TestDocsEvidencePayloadOmitsUnobservedSourceACLState proves a record with no
// observed ACL signal carries NO source_acl_state in the read model: absence
// means "no ACL claim". The reducer must not synthesize a default the collector
// did not assert (correlation-truth, fail-closed). Default-when-unknown is the
// #2164/security-review decision, not the reducer's.
func TestDocsEvidencePayloadOmitsUnobservedSourceACLState(t *testing.T) {
	t.Parallel()

	payload := serviceDocumentationEvidencePayload(ServiceDocumentationRecord{
		SourceSystem:   "confluence",
		SourceRecordID: "section:deploy",
		DocumentID:     "doc:runbook",
		FactKind:       facts.DocumentationClaimCandidateFactKind,
		// SourceACLState intentionally empty: no access-posture signal observed.
	})
	if got, present := payload["source_acl_state"]; present {
		t.Fatalf("unobserved source_acl_state must be omitted, got %#v", got)
	}
}

// TestDocsEvidencePayloadDropsUnknownSourceACLState proves a non-bounded ACL
// value is never carried forward: an unrecognized state is dropped (omitted)
// rather than projected, so a corrupt or future value can never surface as an
// authoritative ACL claim downstream. This is the no-upgrade / fail-closed
// invariant: nothing that is not a known bounded state may be projected.
func TestDocsEvidencePayloadDropsUnknownSourceACLState(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"unknown", "ALLOWED", "hidden", "permission_hidden", "  "} {
		bad := bad
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			payload := serviceDocumentationEvidencePayload(ServiceDocumentationRecord{
				SourceSystem:   "confluence",
				SourceRecordID: "section:deploy",
				DocumentID:     "doc:runbook",
				FactKind:       facts.DocumentationEntityMentionFactKind,
				SourceACLState: bad,
			})
			if got, present := payload["source_acl_state"]; present {
				t.Fatalf("non-bounded source_acl_state %q must be dropped, got %#v", bad, got)
			}
		})
	}
}

// TestDocsEvidenceSourceACLStateFlipsRowHash proves source_acl_state is part of
// the durable observable payload that drives updated-vs-unchanged classification:
// a changed ACL state must change the row payload (so the generation flips), and
// an identical ACL state must stay unchanged (anti-churn). A denied/partial read
// must never hash identically to an allowed read.
func TestDocsEvidenceSourceACLStateFlipsRowHash(t *testing.T) {
	t.Parallel()

	base := ServiceDocumentationRecord{
		SourceSystem:   "confluence",
		SourceRecordID: "section:deploy",
		DocumentID:     "doc:runbook",
		FactKind:       facts.DocumentationEntityMentionFactKind,
		SourceACLState: facts.SourceACLStateAllowed,
	}
	denied := base
	denied.SourceACLState = facts.SourceACLStateDenied

	allowedRows := buildServiceDocumentationEvidence([]ServiceDocumentationRecord{base})
	deniedRows := buildServiceDocumentationEvidence([]ServiceDocumentationRecord{denied})
	if len(allowedRows) != 1 || len(deniedRows) != 1 {
		t.Fatalf("expected one row each, got allowed=%d denied=%d", len(allowedRows), len(deniedRows))
	}
	allowedHash := ServiceEvidencePayloadHash(allowedRows[0].Payload)
	deniedHash := ServiceEvidencePayloadHash(deniedRows[0].Payload)
	if allowedHash == deniedHash {
		t.Fatal("allowed and denied source_acl_state must not hash identically (fail-closed, distinct rows)")
	}

	// Identical ACL state across re-materializations must stay unchanged.
	againRows := buildServiceDocumentationEvidence([]ServiceDocumentationRecord{base})
	if ServiceEvidencePayloadHash(againRows[0].Payload) != allowedHash {
		t.Fatal("identical source_acl_state must hash identically (anti-churn)")
	}
}
