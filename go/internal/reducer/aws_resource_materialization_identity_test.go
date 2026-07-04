// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractCloudResourceNodeRowsRequiresIdentity proves an aws_resource fact
// whose payload is present and non-null on every REQUIRED key but which lacks
// the DERIVED identity needed to form a stable uid (no resource_id and no arn,
// or an empty resource_type) is dropped without a row AND without a quarantine:
// it is a valid-but-incomplete fact, not a malformed (missing-required-field)
// one, so it must not dead-letter. The assertions run unconditionally — the
// prior form guarded them behind `if len(rows) != 0`, so they were dead code
// that passed while checking nothing (a false green).
func TestExtractCloudResourceNodeRowsRequiresIdentity(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		// Every required key present and non-empty, but no resource_id and no arn:
		// no stable identity can be derived, so the fact is dropped, not
		// quarantined.
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
			"resource_id":   "",
			"arn":           "",
		}),
		// Every required key present, but resource_type is present-and-empty (a
		// valid observed value that still cannot key a node): dropped, not
		// quarantined.
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "",
			"resource_id":   "vpc-123",
		}),
	}

	rows, quarantined, err := ExtractCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for incomplete-but-valid identity", len(rows))
	}
	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; an incomplete-but-valid fact must be dropped, not dead-lettered as input_invalid", len(quarantined))
	}
}

// TestExtractCloudResourceNodeRowsQuarantinesMissingRequiredField proves the
// counterpart contract: a fact MISSING a required key (account_id absent, not
// empty) is quarantined as an input_invalid per-fact dead-letter and produces no
// row, while a valid sibling in the same batch still projects. This is the
// positive assertion the false-green form never made.
func TestExtractCloudResourceNodeRowsQuarantinesMissingRequiredField(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		// account_id absent → malformed → quarantined.
		awsResourceEnvelope(map[string]any{
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
			"resource_id":   "vpc-bad",
		}),
		// Fully valid → must still project one row.
		awsResourceEnvelope(map[string]any{
			"account_id":    "111122223333",
			"region":        "us-east-1",
			"resource_type": "aws_ec2_vpc",
			"resource_id":   "vpc-good",
		}),
	}

	rows, quarantined, err := ExtractCloudResourceNodeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil (per-fact isolation, not batch abort)", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; the valid fact must still project despite the malformed sibling", len(rows))
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-account_id fact must be quarantined", len(quarantined))
	}
	if quarantined[0].factKind != facts.AWSResourceFactKind {
		t.Fatalf("quarantined factKind = %q, want %q", quarantined[0].factKind, facts.AWSResourceFactKind)
	}
	if quarantined[0].field != "account_id" {
		t.Fatalf("quarantined field = %q, want %q", quarantined[0].field, "account_id")
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined classification = %q, want %q", quarantined[0].classification, "input_invalid")
	}
}
