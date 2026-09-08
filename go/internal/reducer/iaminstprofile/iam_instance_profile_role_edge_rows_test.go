// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iaminstprofile

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// Envelope and uid fixtures live in iaminstprofile_test_helpers_test.go:
// Go test files cannot share unexported symbols across the reducer-root
// package boundary, so they are duplicated there verbatim (issue #6061).

func TestExtractIAMInstanceProfileRoleEdgeRowsResolvesRoles(t *testing.T) {
	t.Parallel()

	const acct = "123456789012"
	roleA := "arn:aws:iam::" + acct + ":role/app"
	roleB := "arn:aws:iam::" + acct + ":role/breakglass"
	envelopes := []facts.Envelope{
		iamInstanceProfileResourceEnvelope(acct, "app-profile", roleA, roleB),
		iamRoleEnvelope(acct, roleA),
		iamRoleEnvelope(acct, roleB),
	}

	rows, tally, _, err := ExtractIAMInstanceProfileRoleEdgeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractIAMInstanceProfileRoleEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	wantProfileUID := iamInstanceProfileUID(acct, "app-profile")
	wantRoles := map[string]struct{}{
		iamRoleUID(acct, "app"):        {},
		iamRoleUID(acct, "breakglass"): {},
	}
	for _, row := range rows {
		if got := payloadcore.AnyToString(row["profile_uid"]); got != wantProfileUID {
			t.Fatalf("profile_uid = %q, want %q", got, wantProfileUID)
		}
		roleUID := payloadcore.AnyToString(row["role_uid"])
		if _, ok := wantRoles[roleUID]; !ok {
			t.Fatalf("unexpected role_uid %q", roleUID)
		}
		delete(wantRoles, roleUID)
		if got := payloadcore.AnyToString(row["relationship_type"]); got != "HAS_ROLE" {
			t.Fatalf("relationship_type = %q, want HAS_ROLE", got)
		}
		if got := payloadcore.AnyToString(row["resolution_mode"]); got != iamInstanceProfileRoleModeARN {
			t.Fatalf("resolution_mode = %q, want %q", got, iamInstanceProfileRoleModeARN)
		}
	}
	if len(wantRoles) != 0 {
		t.Fatalf("unresolved expected role uids: %v", wantRoles)
	}
	if got := tally.resolved[iamInstanceProfileRoleModeARN]; got != 2 {
		t.Fatalf("resolved[arn] = %d, want 2", got)
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped = %d, want 0", tally.totalSkipped())
	}
}

func TestExtractIAMInstanceProfileRoleEdgeRowsEmptyRolesNoEdge(t *testing.T) {
	t.Parallel()

	rows, tally, _, err := ExtractIAMInstanceProfileRoleEdgeRows([]facts.Envelope{
		iamInstanceProfileResourceEnvelope("123456789012", "app-profile"),
	})
	if err != nil {
		t.Fatalf("ExtractIAMInstanceProfileRoleEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for profile with no roles", len(rows))
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped = %d, want 0 because no role is named", tally.totalSkipped())
	}
}

// TestExtractIAMInstanceProfileRoleEdgeRowsMalformedRoleARNsQuarantines proves
// the #4631 typed-attribute-decode fix: an instance-profile fact whose
// role_arns array contains a present-but-non-string entry must dead-letter as
// a visible input_invalid quarantine, not silently drop the malformed entry
// and continue with whatever role_arns remained valid — a dropped ARN would
// otherwise produce a silently missing HAS_ROLE edge indistinguishable from an
// unscanned-role skip.
func TestExtractIAMInstanceProfileRoleEdgeRowsMalformedRoleARNsQuarantines(t *testing.T) {
	t.Parallel()

	const acct = "123456789012"
	roleA := "arn:aws:iam::" + acct + ":role/app"
	envelopes := []facts.Envelope{
		{
			FactID:   "bad-profile",
			FactKind: facts.AWSResourceFactKind,
			Payload: map[string]any{
				"account_id":    acct,
				"region":        "aws-global",
				"resource_type": "aws_iam_instance_profile",
				"resource_id":   "arn:aws:iam::" + acct + ":instance-profile/bad-profile",
				"attributes": map[string]any{
					"role_arns": []any{roleA, 42},
				},
			},
		},
		iamRoleEnvelope(acct, roleA),
	}

	rows, _, quarantined, err := ExtractIAMInstanceProfileRoleEdgeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractIAMInstanceProfileRoleEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for a quarantined profile fact", len(rows))
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1 for malformed role_arns", len(quarantined))
	}
	if quarantined[0].Classification != "input_invalid" {
		t.Fatalf("quarantined[0].Classification = %q, want input_invalid", quarantined[0].Classification)
	}
}

func TestExtractIAMInstanceProfileRoleEdgeRowsUnscannedRoleSkipped(t *testing.T) {
	t.Parallel()

	const acct = "123456789012"
	unscanned := "arn:aws:iam::999988887777:role/external"
	rows, tally, _, err := ExtractIAMInstanceProfileRoleEdgeRows([]facts.Envelope{
		iamInstanceProfileResourceEnvelope(acct, "app-profile", unscanned),
	})
	if err != nil {
		t.Fatalf("ExtractIAMInstanceProfileRoleEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for unscanned target role", len(rows))
	}
	if got := tally.skipped[iamInstanceProfileRoleSkipTargetUnresolved]; got != 1 {
		t.Fatalf("skipped[target_unresolved] = %d, want 1", got)
	}
	if tally.totalSkipped() != 1 {
		t.Fatalf("totalSkipped = %d, want 1", tally.totalSkipped())
	}
}

func TestExtractIAMInstanceProfileRoleEdgeRowsDuplicateInputOneEdge(t *testing.T) {
	t.Parallel()

	const acct = "123456789012"
	roleARN := "arn:aws:iam::" + acct + ":role/app"
	rows, _, _, err := ExtractIAMInstanceProfileRoleEdgeRows([]facts.Envelope{
		iamInstanceProfileResourceEnvelope(acct, "app-profile", roleARN, roleARN),
		iamInstanceProfileResourceEnvelope(acct, "app-profile", roleARN),
		iamRoleEnvelope(acct, roleARN),
		iamRoleEnvelope(acct, roleARN),
	})
	if err != nil {
		t.Fatalf("ExtractIAMInstanceProfileRoleEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 idempotent edge", len(rows))
	}
}
