// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSourceACLDispositionForMapsBoundedStateToDisposition proves the bounded
// source_acl_state maps onto the approved disclosure dispositions (#2164) and
// that the binary per-caller read decision is the authoritative axis: a
// binary-denied caller is access-denied regardless of a bounded "allowed"
// observation, and the bounded posture drives partial/stale/missing for a
// readable caller.
func TestSourceACLDispositionForMapsBoundedStateToDisposition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		boundedState    string
		binaryReadable  bool
		wantDisposition string
		wantDenied      bool
		wantWithheld    bool
	}{
		{"allowed visible", facts.SourceACLStateAllowed, true, accessDispositionVisible, false, false},
		{"no claim visible", "", true, accessDispositionVisible, false, false},
		{"denied withholds", facts.SourceACLStateDenied, true, accessDispositionDenied, true, true},
		{"partial withholds", facts.SourceACLStatePartial, true, accessDispositionPartial, false, true},
		{"stale visible", facts.SourceACLStateStale, true, accessDispositionStale, false, false},
		{"missing empty", facts.SourceACLStateMissing, true, accessDispositionMissing, false, false},
		// Binary-denied caller is access-denied even when the source observed allowed.
		{"binary denied overrides allowed", facts.SourceACLStateAllowed, false, accessDispositionDenied, true, true},
		{"binary denied overrides stale", facts.SourceACLStateStale, false, accessDispositionDenied, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sourceACLDispositionFor(tc.boundedState, tc.binaryReadable)
			if got.disposition != tc.wantDisposition {
				t.Fatalf("disposition = %q, want %q", got.disposition, tc.wantDisposition)
			}
			if got.permissionDenied != tc.wantDenied {
				t.Fatalf("permissionDenied = %v, want %v", got.permissionDenied, tc.wantDenied)
			}
			if got.contentWithheld != tc.wantWithheld {
				t.Fatalf("contentWithheld = %v, want %v", got.contentWithheld, tc.wantWithheld)
			}
		})
	}
}

// TestApplySourceACLDisclosureWithholdsContentForDenied proves a denied row is
// stripped of every protected content field while its identity and bounded
// state/freshness axes survive, and that the access markers are set. Content is
// never leaked.
func TestApplySourceACLDisclosureWithholdsContentForDenied(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"finding_id":      "finding:denied",
		"finding_type":    "service_deployment_drift",
		"status":          "conflict",
		"freshness_state": "fresh",
		"truth_level":     "derived",
		"summary":         "SECRET deployment drift content",
		"bounded_excerpt": map[string]any{"text": "SECRET excerpt"},
		"acl_summary":     map[string]any{"source_acl_state": "denied"},
	}
	disp := applySourceACLDisclosure(row, true)

	if disp.disposition != accessDispositionDenied {
		t.Fatalf("disposition = %q, want %q", disp.disposition, accessDispositionDenied)
	}
	if row[accessDispositionResponseKey] != accessDispositionDenied {
		t.Fatalf("row access_disposition = %#v, want %q", row[accessDispositionResponseKey], accessDispositionDenied)
	}
	if row[permissionDeniedResponseKey] != true {
		t.Fatalf("row permission_denied = %#v, want true", row[permissionDeniedResponseKey])
	}
	if row[contentWithheldResponseKey] != true {
		t.Fatalf("row content_withheld = %#v, want true", row[contentWithheldResponseKey])
	}
	if _, leaked := row["summary"]; leaked {
		t.Fatalf("denied row leaked protected content 'summary': %#v", row)
	}
	if _, leaked := row["bounded_excerpt"]; leaked {
		t.Fatalf("denied row leaked protected content 'bounded_excerpt': %#v", row)
	}
	// #2138: freshness/truth axes preserved on the withheld row, not collapsed.
	if row["freshness_state"] != "fresh" {
		t.Fatalf("freshness_state = %#v, want preserved 'fresh'", row["freshness_state"])
	}
	if row["truth_level"] != "derived" {
		t.Fatalf("truth_level = %#v, want preserved 'derived'", row["truth_level"])
	}
	if row["source_acl_state"] != "denied" {
		t.Fatalf("source_acl_state = %#v, want 'denied'", row["source_acl_state"])
	}
	if row["finding_id"] != "finding:denied" {
		t.Fatalf("finding_id = %#v, want preserved identity", row["finding_id"])
	}
}

// TestApplySourceACLDisclosureWithholdsContentForPartial proves a partial row is
// surfaced with a partial marker and its content boundaries respected (content
// withheld), but is not flagged permission_denied.
func TestApplySourceACLDisclosureWithholdsContentForPartial(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"finding_id":  "finding:partial",
		"summary":     "partially restricted content",
		"acl_summary": map[string]any{"source_acl_state": "partial"},
	}
	disp := applySourceACLDisclosure(row, true)

	if disp.disposition != accessDispositionPartial {
		t.Fatalf("disposition = %q, want %q", disp.disposition, accessDispositionPartial)
	}
	if row[contentWithheldResponseKey] != true {
		t.Fatalf("partial content_withheld = %#v, want true", row[contentWithheldResponseKey])
	}
	if _, present := row[permissionDeniedResponseKey]; present {
		t.Fatalf("partial row must not set permission_denied: %#v", row)
	}
	if _, leaked := row["summary"]; leaked {
		t.Fatalf("partial row leaked content: %#v", row)
	}
	if row["source_acl_state"] != "partial" {
		t.Fatalf("source_acl_state = %#v, want 'partial'", row["source_acl_state"])
	}
}

// TestApplySourceACLDisclosureKeepsContentForStaleAndAllowed proves a stale
// (permitted-but-stale) and an allowed row keep their content; stale is surfaced
// on the distinct ACL axis without withholding.
func TestApplySourceACLDisclosureKeepsContentForStaleAndAllowed(t *testing.T) {
	t.Parallel()

	stale := map[string]any{
		"finding_id":  "finding:stale",
		"summary":     "stale but readable content",
		"acl_summary": map[string]any{"source_acl_state": "stale"},
	}
	disp := applySourceACLDisclosure(stale, true)
	if disp.disposition != accessDispositionStale {
		t.Fatalf("stale disposition = %q, want %q", disp.disposition, accessDispositionStale)
	}
	if _, withheld := stale[contentWithheldResponseKey]; withheld {
		t.Fatalf("stale row must not withhold content: %#v", stale)
	}
	if stale["summary"] != "stale but readable content" {
		t.Fatalf("stale row dropped readable content: %#v", stale)
	}

	allowed := map[string]any{
		"finding_id":  "finding:allowed",
		"summary":     "fully readable content",
		"acl_summary": map[string]any{"source_acl_state": "allowed"},
	}
	disp = applySourceACLDisclosure(allowed, true)
	if disp.disposition != accessDispositionVisible {
		t.Fatalf("allowed disposition = %q, want %q", disp.disposition, accessDispositionVisible)
	}
	if allowed["summary"] != "fully readable content" {
		t.Fatalf("allowed row dropped content: %#v", allowed)
	}
}

// TestApplySourceACLDisclosureReadsNestedPayloadACL proves the bounded ACL claim
// is read from a nested "payload" body when the row wraps the fact (semantic
// evidence / documentation facts shape).
func TestApplySourceACLDisclosureReadsNestedPayloadACL(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"fact_id": "fact:1",
		"payload": map[string]any{
			"acl_summary": map[string]any{"source_acl_state": "denied"},
		},
	}
	disp := applySourceACLDisclosure(row, true)
	if disp.disposition != accessDispositionDenied {
		t.Fatalf("nested disposition = %q, want %q", disp.disposition, accessDispositionDenied)
	}
	if _, leaked := row["payload"]; leaked {
		t.Fatalf("denied row leaked nested payload content: %#v", row)
	}
	if row["source_acl_state"] != "denied" {
		t.Fatalf("source_acl_state = %#v, want 'denied'", row["source_acl_state"])
	}
}
