// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iampolicy

import (
	"testing"

	iamv1 "github.com/eshu-hq/eshu-sdk/factschema/iam/v1"
)

// TestCov8PrincipalGrantAllows pins the trusted-action matcher: exact hits,
// the two unambiguous wildcard shapes, and the conservative refusal to
// expand partial wildcards such as "iam:Create*".
func TestCov8PrincipalGrantAllows(t *testing.T) {
	t.Parallel()

	grant := PrincipalGrant{TrustedActions: map[string]struct{}{
		"iam:PassRole": {},
		"ec2:*":        {},
		"*":            {},
	}}
	for _, action := range []string{"iam:PassRole", "ec2:RunInstances", "s3:GetObject"} {
		if !grant.Allows(action) {
			t.Errorf("Allows(%q) = false, want true", action)
		}
	}

	scoped := PrincipalGrant{TrustedActions: map[string]struct{}{"ec2:*": {}}}
	if !scoped.Allows("ec2:RunInstances") {
		t.Error("Allows(ec2:RunInstances) with ec2:* = false, want true")
	}
	if scoped.Allows("iam:PassRole") {
		t.Error("Allows(iam:PassRole) with ec2:* = true, want false")
	}
	// A partial wildcard must not expand: it is neither exact, "*", nor
	// "service:*".
	partial := PrincipalGrant{TrustedActions: map[string]struct{}{"iam:Create*": {}}}
	if partial.Allows("iam:CreateRole") {
		t.Error("Allows(iam:CreateRole) with iam:Create* = true, want false (conservative)")
	}
	empty := PrincipalGrant{}
	if empty.Allows("iam:PassRole") {
		t.Error("Allows on empty grant = true, want false")
	}
	if empty.Allows("no-colon-action") {
		t.Error("Allows on colon-less action = true, want false")
	}
}

// TestCov8PrincipalGrantStatementsCovering pins carrier-action resolution:
// a concrete lookup finds exact, "*", and "service:*" registrations, and a
// statement registered under several keys is returned once.
func TestCov8PrincipalGrantStatementsCovering(t *testing.T) {
	t.Parallel()

	exact := Statement{FactID: "fact-exact", Permission: iamv1.Permission{Resources: []string{"*"}}}
	wild := Statement{FactID: "fact-wild", Permission: iamv1.Permission{Resources: []string{"*"}}}
	service := Statement{FactID: "fact-service", Permission: iamv1.Permission{Resources: []string{"*"}}}
	grant := PrincipalGrant{StatementsByAction: map[string][]Statement{
		"iam:PassRole": {exact, wild},
		"*":            {wild},
		"iam:*":        {service},
	}}
	got := grant.StatementsCovering("iam:PassRole")
	if len(got) != 3 {
		t.Fatalf("len(StatementsCovering) = %d, want 3 (exact + star + service wildcard, deduped)", len(got))
	}
	seen := map[string]int{}
	for _, statement := range got {
		seen[statement.FactID]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("statement %q appears %d times, want once", id, count)
		}
	}

	// Statements without a FactID cannot dedup and are all returned.
	anonymous := Statement{Permission: iamv1.Permission{Resources: []string{"*"}}}
	anonGrant := PrincipalGrant{StatementsByAction: map[string][]Statement{
		"s3:GetObject": {anonymous},
		"*":            {anonymous},
	}}
	if got := anonGrant.StatementsCovering("s3:GetObject"); len(got) != 2 {
		t.Errorf("len(anonymous StatementsCovering) = %d, want 2", len(got))
	}
	if got := (PrincipalGrant{}).StatementsCovering("iam:PassRole"); len(got) != 0 {
		t.Errorf("len(empty StatementsCovering) = %d, want 0", len(got))
	}
}

// TestCov8PrincipalGrantDenied pins Deny matching: exact, "*", and
// "service:*" deny shapes remove the principal from the action.
func TestCov8PrincipalGrantDenied(t *testing.T) {
	t.Parallel()

	grant := PrincipalGrant{DenyActions: map[string]struct{}{
		"s3:GetObject": {},
		"ec2:*":        {},
	}}
	if !grant.Denied("s3:GetObject") {
		t.Error("Denied(s3:GetObject) = false, want true")
	}
	if !grant.Denied("ec2:RunInstances") {
		t.Error("Denied(ec2:RunInstances) with ec2:* = false, want true")
	}
	if grant.Denied("iam:PassRole") {
		t.Error("Denied(iam:PassRole) = true, want false")
	}
	star := PrincipalGrant{DenyActions: map[string]struct{}{"*": {}}}
	if !star.Denied("anything:AtAll") {
		t.Error("Denied with * = false, want true")
	}
	if (PrincipalGrant{}).Denied("iam:PassRole") {
		t.Error("Denied on empty grant = true, want false")
	}
}

// TestCov8AllowStatementTouches pins the action-set matcher shared by
// primitive evaluation.
func TestCov8AllowStatementTouches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		actions []string
		target  string
		want    bool
	}{
		{"exact", []string{"iam:PassRole"}, "iam:PassRole", true},
		{"star", []string{"*"}, "iam:PassRole", true},
		{"service star", []string{"iam:*"}, "iam:PassRole", true},
		{"other service", []string{"ec2:*"}, "iam:PassRole", false},
		{"miss", []string{"s3:GetObject"}, "iam:PassRole", false},
		{"empty", nil, "iam:PassRole", false},
		{"colon-less target with star", []string{"*"}, "PassRole", true},
		{"colon-less target miss", []string{"iam:*"}, "PassRole", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := AllowStatementTouches(tc.actions, tc.target); got != tc.want {
				t.Errorf("AllowStatementTouches(%v, %q) = %v, want %v", tc.actions, tc.target, got, tc.want)
			}
		})
	}
}

// TestCov8StatementTouchesCatalog pins the skip-accounting gate: an
// untrusted statement counts only when one of its actions is a catalog
// action.
func TestCov8StatementTouchesCatalog(t *testing.T) {
	t.Parallel()

	catalog := map[string]struct{}{"iam:PassRole": {}, "ec2:RunInstances": {}}
	if !StatementTouchesCatalog([]string{"iam:PassRole"}, catalog) {
		t.Error("exact catalog action = false, want true")
	}
	if !StatementTouchesCatalog([]string{"*"}, catalog) {
		t.Error("star action = false, want true")
	}
	if !StatementTouchesCatalog([]string{"iam:*"}, catalog) {
		t.Error("service wildcard covering a catalog action = false, want true")
	}
	if StatementTouchesCatalog([]string{"s3:GetObject"}, catalog) {
		t.Error("non-catalog action = true, want false")
	}
	if StatementTouchesCatalog([]string{"other:*"}, catalog) {
		t.Error("non-covering service wildcard = true, want false")
	}
	if StatementTouchesCatalog([]string{"iam:PassRole"}, map[string]struct{}{}) {
		t.Error("action against empty catalog = true, want false")
	}
	if StatementTouchesCatalog([]string{"no-colon"}, catalog) {
		t.Error("colon-less action = true, want false")
	}
	if !StatementTouchesCatalog([]string{"ec2:RunInstances"}, map[string]struct{}{"ec2:*": {}}) {
		t.Error("exact action matching a catalog wildcard = false, want true")
	}
}

// TestCov8CollectTrustedResources pins the union: deduped, byte-sorted,
// verbatim resources across the covering statements.
func TestCov8CollectTrustedResources(t *testing.T) {
	t.Parallel()

	got := CollectTrustedResources([]Statement{
		{FactID: "a", Permission: iamv1.Permission{Resources: []string{"arn:b", "arn:a"}}},
		{FactID: "b", Permission: iamv1.Permission{Resources: []string{"arn:a", "arn:c"}}},
	})
	want := []string{"arn:a", "arn:b", "arn:c"}
	if len(got) != len(want) {
		t.Fatalf("CollectTrustedResources() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CollectTrustedResources() = %v, want %v", got, want)
		}
	}
	if got := CollectTrustedResources(nil); len(got) != 0 {
		t.Errorf("CollectTrustedResources(nil) = %v, want empty", got)
	}
}

// TestCov8ResourceTypeOfARN pins IAM ARN classification, including the
// non-IAM and malformed rejections.
func TestCov8ResourceTypeOfARN(t *testing.T) {
	t.Parallel()

	cases := []struct {
		arn  string
		want string
	}{
		{"arn:aws:iam::123456789012:role/Admin", ResourceTypeRole},
		{"arn:aws:iam::123456789012:user/alice", ResourceTypeUser},
		{"arn:aws:iam::123456789012:policy/ReadOnly", ResourceTypePolicy},
		{"arn:aws:iam::123456789012:group/devs", ResourceTypeGroup},
		{"arn:aws:s3:::my-bucket", ""},
		{"arn:aws:iam::123456789012:instance-profile/web", ""},
		{"not-an-arn", ""},
		{"arn:aws:iam::1234", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ResourceTypeOfARN(tc.arn); got != tc.want {
			t.Errorf("ResourceTypeOfARN(%q) = %q, want %q", tc.arn, got, tc.want)
		}
	}
}

// TestCov8GlobMatch pins the iterative wildcard matcher, including the
// backtracking and trailing-star arms.
func TestCov8GlobMatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "arn:aws:iam::1:role/A", true},
		{"arn:aws:iam::1:role/*", "arn:aws:iam::1:role/A", true},
		{"arn:aws:iam::1:role/*", "arn:aws:iam::1:user/A", false},
		{"role/?dmin", "role/Admin", true},
		{"role/?dmin", "role/Administrator", false},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYYd", false},
		{"exact", "exact", true},
		{"exact", "other", false},
		{"", "", true},
		{"", "x", false},
		{"*", "", true},
		{"***", "anything", true},
		{"a**", "a", true},
	}
	for _, tc := range cases {
		if got := GlobMatch(tc.pattern, tc.value); got != tc.want {
			t.Errorf("GlobMatch(%q, %q) = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}
