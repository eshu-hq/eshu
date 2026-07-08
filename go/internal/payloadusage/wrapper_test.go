// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import "testing"

// fixtureWrapperStatement mirrors the real iam_can_perform / iam_escalation
// handler pattern (#4668): a decoded aws_iam_permission is stored in a wrapper
// struct (iamPermissionStatement) whose `permission` field is typed as the
// seam struct, and the field reads happen two selector levels deep —
// `statement.permission.Actions` — after the wrapper slice is ranged inside a
// helper, or the wrapper is passed by value into one. Before the wrapper-field
// attribution fix, none of these reads were attributed to decodeAWSIAMPermission
// because the scanner only followed `ident.Field` where ident is a seam-bound
// value, not `ident.wrapperField.SeamField`.
const fixtureWrapperStatement = `package reducer

type iamPermissionStatement struct {
	factID     string
	permission iamv1.Permission
}

func buildGrant(statements []iamPermissionStatement) {
	for _, statement := range statements {
		_ = statement.permission.Actions
		if statement.permission.Effect == "Deny" {
			_ = statement.permission.NotResources
		}
	}
}

func inspectOne(statement iamPermissionStatement) {
	_ = statement.permission.Resources
}

func decodeAWSIAMPermission(env facts.Envelope) (iamv1.Permission, error) {
	return iamv1.Permission{}, nil
}
`

func TestScanDecodeUsageFollowsWrapperStructField(t *testing.T) {
	t.Parallel()

	dir := writeFixtureDir(t, map[string]string{
		"iam_can_perform_grant.go": fixtureWrapperStatement,
	})

	seams := []DecodeSeam{{
		FuncName:      "decodeAWSIAMPermission",
		FactKindConst: "FactKindAWSIAMPermission",
		StructPackage: "iamv1",
		StructName:    "Permission",
	}}
	usage, err := ScanDecodeUsage(dir, seams)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	fieldNames := map[string]bool{}
	for _, e := range usage["decodeAWSIAMPermission"] {
		fieldNames[e.GoFieldName] = true
	}

	// Actions/Effect/NotResources are read through the []iamPermissionStatement
	// range var; Resources through the by-value iamPermissionStatement param.
	for _, want := range []string{"Actions", "Effect", "NotResources", "Resources"} {
		if !fieldNames[want] {
			t.Errorf("field %q read through the iamPermissionStatement wrapper was not attributed to decodeAWSIAMPermission; got %+v", want, usage["decodeAWSIAMPermission"])
		}
	}
}

// fixtureWrapperPrincipal mirrors secretsIAMRoleCloudResourceUID
// (secrets_iam_trust_chain_iam_role.go): the aws_iam_principal fields are read
// ONLY through the secretsIAMPrincipal{decoded} wrapper, so before the fix the
// kind's UsedFields was empty even though the handler reads two fields — the
// strongest form of the undercount called out in the #4666 review.
const fixtureWrapperPrincipal = `package reducer

type secretsIAMPrincipal struct {
	env     facts.Envelope
	decoded iamv1.Principal
}

func roleCloudResourceUID(principals []secretsIAMPrincipal) string {
	for _, principal := range principals {
		if principal.decoded.AccountID == "" || principal.decoded.Region == "" {
			continue
		}
		return principal.decoded.AccountID
	}
	return ""
}

func decodeAWSIAMPrincipal(env facts.Envelope) (iamv1.Principal, error) {
	return iamv1.Principal{}, nil
}
`

func TestScanDecodeUsageWrapperAttributesEmptyPrincipalFields(t *testing.T) {
	t.Parallel()

	dir := writeFixtureDir(t, map[string]string{
		"secrets_iam_trust_chain_iam_role.go": fixtureWrapperPrincipal,
	})

	seams := []DecodeSeam{{
		FuncName:      "decodeAWSIAMPrincipal",
		FactKindConst: "FactKindAWSIAMPrincipal",
		StructPackage: "iamv1",
		StructName:    "Principal",
	}}
	usage, err := ScanDecodeUsage(dir, seams)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	fieldNames := map[string]bool{}
	for _, e := range usage["decodeAWSIAMPrincipal"] {
		fieldNames[e.GoFieldName] = true
	}
	for _, want := range []string{"AccountID", "Region"} {
		if !fieldNames[want] {
			t.Errorf("field %q read through the secretsIAMPrincipal wrapper was not attributed to decodeAWSIAMPrincipal; got %+v", want, usage["decodeAWSIAMPrincipal"])
		}
	}
}

// fixtureWrapperNonSeamField proves the attribution is bounded to wrapper
// fields whose TYPE is a seam struct: a read through a non-seam wrapper field
// (factID string) must attribute nothing, and a one-level wrapper read
// (passing the whole struct, `statement.permission`) is not a field read of
// the seam struct.
const fixtureWrapperNonSeamField = `package reducer

type iamPermissionStatement struct {
	factID     string
	permission iamv1.Permission
}

func inspectMeta(statements []iamPermissionStatement) {
	for _, statement := range statements {
		_ = statement.factID
		consume(statement.permission)
	}
}

func consume(p iamv1.Permission) {}

func decodeAWSIAMPermission(env facts.Envelope) (iamv1.Permission, error) {
	return iamv1.Permission{}, nil
}
`

// fixtureWrapperInferredVar covers the inferred `var` declaration shape a
// handler can use instead of `:=` — `var statement = iamPermissionStatement{...}`
// and `var statements = []iamPermissionStatement{...}`, where ValueSpec.Type is
// nil and the type lives on the composite literal in ValueSpec.Values. Without
// binding from the composite literal, the later statement.permission.Actions
// reads would be a false-green for the gate (codex #4954 review, P2).
const fixtureWrapperInferredVar = `package reducer

type iamPermissionStatement struct {
	factID     string
	permission iamv1.Permission
}

func buildFromInferredVars() {
	var one = iamPermissionStatement{}
	_ = one.permission.Actions

	var many = []iamPermissionStatement{}
	for _, statement := range many {
		_ = statement.permission.Effect
	}
}

func decodeAWSIAMPermission(env facts.Envelope) (iamv1.Permission, error) {
	return iamv1.Permission{}, nil
}
`

func TestScanDecodeUsageWrapperFollowsInferredVarDeclarations(t *testing.T) {
	t.Parallel()

	dir := writeFixtureDir(t, map[string]string{
		"inferred.go": fixtureWrapperInferredVar,
	})

	seams := []DecodeSeam{{
		FuncName:      "decodeAWSIAMPermission",
		FactKindConst: "FactKindAWSIAMPermission",
		StructPackage: "iamv1",
		StructName:    "Permission",
	}}
	usage, err := ScanDecodeUsage(dir, seams)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}

	fieldNames := map[string]bool{}
	for _, e := range usage["decodeAWSIAMPermission"] {
		fieldNames[e.GoFieldName] = true
	}
	// Actions via `var one = iamPermissionStatement{}`; Effect via the range
	// over `var many = []iamPermissionStatement{}`.
	for _, want := range []string{"Actions", "Effect"} {
		if !fieldNames[want] {
			t.Errorf("field %q read through an inferred var wrapper declaration was not attributed; got %+v", want, usage["decodeAWSIAMPermission"])
		}
	}
}

func TestScanDecodeUsageWrapperIgnoresNonSeamWrapperField(t *testing.T) {
	t.Parallel()

	dir := writeFixtureDir(t, map[string]string{
		"meta.go": fixtureWrapperNonSeamField,
	})

	seams := []DecodeSeam{{
		FuncName:      "decodeAWSIAMPermission",
		FactKindConst: "FactKindAWSIAMPermission",
		StructPackage: "iamv1",
		StructName:    "Permission",
	}}
	usage, err := ScanDecodeUsage(dir, seams)
	if err != nil {
		t.Fatalf("ScanDecodeUsage() error = %v", err)
	}
	// `consume(statement.permission)` binds the whole seam value into consume's
	// parameter p, so `p`'s body reads (none here) would attribute via the
	// existing parameter path — but the wrapper helper itself reads no seam
	// FIELD, so nothing is attributed from inspectMeta/consume.
	if got := usage["decodeAWSIAMPermission"]; len(got) != 0 {
		t.Fatalf("usage = %+v, want none: only a non-seam wrapper field (factID) and a whole-struct pass are present, no seam field read", got)
	}
}
