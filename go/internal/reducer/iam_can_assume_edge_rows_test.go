// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// iamPermissionEnvelope builds an aws_iam_permission fact envelope for the
// extractor tests. Only the fields the CAN_ASSUME slice consumes are set.
func iamPermissionEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSIAMPermissionFactKind,
		Payload:  payload,
	}
}

// iamRoleEnvelope and iamUserEnvelope build the aws_resource node facts the
// join index resolves against. IAM is a global service: region is "aws-global"
// and resource_id == arn, matching the iam scanner's roleObservation /
// userObservation.
func iamRoleEnvelope(accountID, arn string) facts.Envelope {
	return resourceEnvelope(accountID, "aws-global", "aws_iam_role", arn, arn, arn)
}

func iamUserEnvelope(accountID, arn string) facts.Envelope {
	return resourceEnvelope(accountID, "aws-global", "aws_iam_user", arn, arn, arn)
}

func trustPermissionFact(accountID, roleARN string, assumePrincipals ...string) facts.Envelope {
	principals := make([]any, 0, len(assumePrincipals))
	for _, p := range assumePrincipals {
		principals = append(principals, p)
	}
	return iamPermissionEnvelope(map[string]any{
		"account_id":        accountID,
		"region":            "aws-global",
		"principal_arn":     roleARN,
		"principal_type":    "aws_iam_role",
		"policy_source":     "trust",
		"effect":            "Allow",
		"assume_principals": principals,
	})
}

func TestExtractIAMCanAssumeEdgeRowsResolvesRoleAndUser(t *testing.T) {
	t.Parallel()

	const acct = "123456789012"
	roleARN := "arn:aws:iam::123456789012:role/eshu-runtime"
	assumingRoleARN := "arn:aws:iam::123456789012:role/ci-deployer"
	assumingUserARN := "arn:aws:iam::123456789012:user/breakglass"

	resources := []facts.Envelope{
		iamRoleEnvelope(acct, roleARN),
		iamRoleEnvelope(acct, assumingRoleARN),
		iamUserEnvelope(acct, assumingUserARN),
	}
	perms := []facts.Envelope{
		trustPermissionFact(acct, roleARN, assumingRoleARN, assumingUserARN),
	}

	rows, tally := ExtractIAMCanAssumeEdgeRows(resources, perms)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	roleUID := cloudResourceUID(acct, "aws-global", "aws_iam_role", roleARN)
	wantPrincipalUIDs := map[string]string{
		cloudResourceUID(acct, "aws-global", "aws_iam_role", assumingRoleARN): iamCanAssumePrincipalKindRole,
		cloudResourceUID(acct, "aws-global", "aws_iam_user", assumingUserARN): iamCanAssumePrincipalKindUser,
	}
	for _, row := range rows {
		if got := anyToString(row["role_uid"]); got != roleUID {
			t.Fatalf("role_uid = %q, want %q", got, roleUID)
		}
		principalUID := anyToString(row["principal_uid"])
		wantKind, ok := wantPrincipalUIDs[principalUID]
		if !ok {
			t.Fatalf("unexpected principal_uid %q", principalUID)
		}
		if got := anyToString(row["principal_kind"]); got != wantKind {
			t.Fatalf("principal_kind = %q, want %q for %q", got, wantKind, principalUID)
		}
		if got := anyToString(row["resolution_mode"]); got != iamCanAssumeModeARN {
			t.Fatalf("resolution_mode = %q, want %q", got, iamCanAssumeModeARN)
		}
		delete(wantPrincipalUIDs, principalUID)
	}
	if len(wantPrincipalUIDs) != 0 {
		t.Fatalf("unresolved expected principals: %v", wantPrincipalUIDs)
	}
	if got := tally.resolved[iamCanAssumePrincipalKindRole]; got != 1 {
		t.Fatalf("tally.resolved[role] = %d, want 1", got)
	}
	if got := tally.resolved[iamCanAssumePrincipalKindUser]; got != 1 {
		t.Fatalf("tally.resolved[user] = %d, want 1", got)
	}
}

func TestExtractIAMCanAssumeEdgeRowsSkipsExternalServiceAndWildcard(t *testing.T) {
	t.Parallel()

	const acct = "123456789012"
	roleARN := "arn:aws:iam::123456789012:role/eshu-runtime"

	resources := []facts.Envelope{iamRoleEnvelope(acct, roleARN)}
	perms := []facts.Envelope{
		trustPermissionFact(
			acct, roleARN,
			"*",                              // wildcard principal
			"ec2.amazonaws.com",              // AWS service principal
			"123456789012",                   // bare account id
			"arn:aws:iam::123456789012:root", // account root
			"arn:aws:iam::999988887777:role/unscanned",   // cross-account, not scanned
			"arn:aws:iam::aws:policy/SomethingFederated", // not a principal node
		),
	}

	rows, tally := ExtractIAMCanAssumeEdgeRows(resources, perms)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 (all principals external/wildcard/unscanned)", len(rows))
	}
	if got := tally.skipped[iamCanAssumeSkipWildcard]; got != 1 {
		t.Fatalf("tally.skipped[wildcard] = %d, want 1", got)
	}
	// ec2.amazonaws.com, bare account id, account root, and aws policy arn are
	// all non-resolvable non-role/user principals. The bare account id and
	// service principal are not ARNs; root and the policy arn are ARNs that do
	// not resolve to a scanned role/user node.
	nonWildcardSkips := tally.skipped[iamCanAssumeSkipServiceOrAccount] + tally.skipped[iamCanAssumeSkipExternalUnresolved]
	if nonWildcardSkips != 5 {
		t.Fatalf("non-wildcard skips = %d, want 5", nonWildcardSkips)
	}
}

func TestExtractIAMCanAssumeEdgeRowsSkipsDenyAndUnresolvedSource(t *testing.T) {
	t.Parallel()

	const acct = "123456789012"
	roleARN := "arn:aws:iam::123456789012:role/eshu-runtime"
	assumingARN := "arn:aws:iam::123456789012:role/ci-deployer"

	resources := []facts.Envelope{
		iamRoleEnvelope(acct, roleARN),
		iamRoleEnvelope(acct, assumingARN),
	}

	// Deny trust statement must not produce an edge.
	denyFact := iamPermissionEnvelope(map[string]any{
		"account_id":        acct,
		"principal_arn":     roleARN,
		"policy_source":     "trust",
		"effect":            "Deny",
		"assume_principals": []any{assumingARN},
	})
	// Trust fact whose own principal_arn (the role) was not scanned.
	unscannedRoleFact := trustPermissionFact(acct, "arn:aws:iam::123456789012:role/ghost", assumingARN)
	// Non-trust source must be ignored entirely.
	inlineFact := iamPermissionEnvelope(map[string]any{
		"account_id":    acct,
		"principal_arn": roleARN,
		"policy_source": "inline",
		"effect":        "Allow",
		"actions":       []any{"iam:passrole"},
		"resources":     []any{"*"},
	})

	rows, tally := ExtractIAMCanAssumeEdgeRows(resources, []facts.Envelope{denyFact, unscannedRoleFact, inlineFact})
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if got := tally.skipped[iamCanAssumeSkipDeny]; got != 1 {
		t.Fatalf("tally.skipped[deny] = %d, want 1", got)
	}
	if got := tally.skipped[iamCanAssumeSkipSourceUnresolved]; got != 1 {
		t.Fatalf("tally.skipped[source_unresolved] = %d, want 1", got)
	}
}

func TestExtractIAMCanAssumeEdgeRowsSelfAssumeAndDuplicateDedupe(t *testing.T) {
	t.Parallel()

	const acct = "123456789012"
	roleARN := "arn:aws:iam::123456789012:role/eshu-runtime"
	assumingARN := "arn:aws:iam::123456789012:role/ci-deployer"

	resources := []facts.Envelope{
		iamRoleEnvelope(acct, roleARN),
		iamRoleEnvelope(acct, assumingARN),
	}
	// Self-assume (role trusts itself) plus the same external principal twice
	// across two facts, plus a duplicate within one fact.
	perms := []facts.Envelope{
		trustPermissionFact(acct, roleARN, roleARN, assumingARN, assumingARN),
		trustPermissionFact(acct, roleARN, assumingARN),
	}

	rows, _ := ExtractIAMCanAssumeEdgeRows(resources, perms)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (self-assume skipped, duplicates deduped)", len(rows))
	}
	wantPrincipal := cloudResourceUID(acct, "aws-global", "aws_iam_role", assumingARN)
	if got := anyToString(rows[0]["principal_uid"]); got != wantPrincipal {
		t.Fatalf("principal_uid = %q, want %q", got, wantPrincipal)
	}
}

func TestExtractIAMCanAssumeEdgeRowsCrossAccountResolvesWhenScanned(t *testing.T) {
	t.Parallel()

	const homeAcct = "123456789012"
	const otherAcct = "999988887777"
	roleARN := "arn:aws:iam::123456789012:role/eshu-runtime"
	crossAccountRole := "arn:aws:iam::999988887777:role/partner"

	// The other account's role WAS scanned in this scope generation, so the
	// cross-account trust edge resolves (trust-boundary rule: only resolves
	// because the node exists, never fabricated).
	resources := []facts.Envelope{
		iamRoleEnvelope(homeAcct, roleARN),
		iamRoleEnvelope(otherAcct, crossAccountRole),
	}
	perms := []facts.Envelope{trustPermissionFact(homeAcct, roleARN, crossAccountRole)}

	rows, _ := ExtractIAMCanAssumeEdgeRows(resources, perms)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 cross-account edge", len(rows))
	}
	wantPrincipal := cloudResourceUID(otherAcct, "aws-global", "aws_iam_role", crossAccountRole)
	if got := anyToString(rows[0]["principal_uid"]); got != wantPrincipal {
		t.Fatalf("principal_uid = %q, want %q", got, wantPrincipal)
	}
}

func TestExtractIAMCanAssumeEdgeRowsEmptyInput(t *testing.T) {
	t.Parallel()

	rows, tally := ExtractIAMCanAssumeEdgeRows(nil, nil)
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped = %d, want 0", tally.totalSkipped())
	}
}
