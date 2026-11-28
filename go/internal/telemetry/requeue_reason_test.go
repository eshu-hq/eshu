// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"strings"
	"testing"
)

// TestRequeueReasonVocabularyIsClosed pins the #11615 writer vocabulary: every
// declared reason validates, and the forbidden values (empty, unknown, and
// the legacy_unclassified display label) do not.
func TestRequeueReasonVocabularyIsClosed(t *testing.T) {
	t.Parallel()
	for _, r := range AllRequeueReasons() {
		if err := r.Validate(); err != nil {
			t.Fatalf("vocabulary member %q fails validation: %v", string(r), err)
		}
		if strings.ContainsAny(string(r), " '\";") || strings.TrimSpace(string(r)) != string(r) {
			t.Fatalf("reason %q is not a safe bare SQL literal", string(r))
		}
	}
	for _, bad := range []RequeueReason{"", "unknown", RequeueReason(RequeueReasonLegacyUnclassified), "pending", "RETRY_BACKOFF"} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("forbidden reason %q validates", string(bad))
		}
	}
}

// TestRequeueReasonSQLLiteralEmbedsQuotedLabel proves the SQL literal is the
// single-quoted reason, so writers can embed it without renumbering query
// args.
func TestRequeueReasonSQLLiteralEmbedsQuotedLabel(t *testing.T) {
	t.Parallel()
	if got := RequeueReasonRetryBackoff.SQLLiteral(); got != "'retry_backoff'" {
		t.Fatalf("SQL literal = %q, want 'retry_backoff'", got)
	}
}

// TestRequeueReasonSQLLiteralPanicsOnUnknown proves a bad call site fails
// loudly in tests instead of writing a corrupt label.
func TestRequeueReasonSQLLiteralPanicsOnUnknown(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("SQLLiteral on an unknown reason did not panic")
		}
	}()
	RequeueReason("bogus").SQLLiteral()
}

// TestDisplayRequeueReasonMapsNullToLegacy proves readers display NULL as
// legacy_unclassified while passing real reasons through.
func TestDisplayRequeueReasonMapsNullToLegacy(t *testing.T) {
	t.Parallel()
	if got := DisplayRequeueReason(nil); got != RequeueReasonLegacyUnclassified {
		t.Fatalf("nil displays as %q, want legacy_unclassified", got)
	}
	stored := "waiting_wake"
	if got := DisplayRequeueReason(&stored); got != "waiting_wake" {
		t.Fatalf("stored reason displays as %q, want waiting_wake", got)
	}
}
