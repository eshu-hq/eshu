// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relguard_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
)

// writeFixtureTree writes a minimal awscloud-shaped source tree under a temp dir
// and returns the awscloud and services directories. It lets the guard run
// against controlled inputs, including the negative cases the guard must catch,
// without touching the live tree.
func writeFixtureTree(t *testing.T, constantsSrc string, services map[string]string) (awscloudDir, servicesDir string) {
	t.Helper()
	awscloudDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(awscloudDir, "constants_fixture.go"), []byte(constantsSrc), 0o600); err != nil {
		t.Fatalf("write constants fixture: %v", err)
	}
	servicesDir = filepath.Join(awscloudDir, "services")
	for service, relationshipsSrc := range services {
		dir := filepath.Join(servicesDir, service)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "relationships.go"), []byte(relationshipsSrc), 0o600); err != nil {
			t.Fatalf("write relationships fixture: %v", err)
		}
	}
	return awscloudDir, servicesDir
}

const fixtureConstants = `package awscloud

const (
	ResourceTypeEC2VPC = "aws_ec2_vpc"
	ResourceTypeS3Bucket = "aws_s3_bucket"
	ResourceTypeGeneric = "aws_resource"
)
`

// relationshipsHeader is the fixture scanner-package preamble. The fixture
// declares a local RelationshipObservation type so the AST matcher (which keys
// on the type name, not the import) sees the same shape as real scanner code.
const relationshipsHeader = `package fixture

type RelationshipObservation struct {
	RelationshipType string
	TargetResourceID string
	TargetARN        string
	TargetType       string
}

`

// relationshipsHeaderWithImport is relationshipsHeader for fixtures that
// reference a qualified awscloud.ResourceType* selector. The import must precede
// the type declaration to parse.
const relationshipsHeaderWithImport = `package fixture

import awscloud "x"

type RelationshipObservation struct {
	RelationshipType string
	TargetResourceID string
	TargetARN        string
	TargetType       string
}

`

// TestDeclaredResourceTypeValues proves the static layer reads every
// ResourceType constant value from the awscloud source and nothing else.
func TestDeclaredResourceTypeValues(t *testing.T) {
	awscloudDir, _ := writeFixtureTree(t, fixtureConstants, nil)
	values, err := relguard.DeclaredResourceTypeValues(awscloudDir)
	if err != nil {
		t.Fatalf("DeclaredResourceTypeValues() error = %v", err)
	}
	want := map[string]bool{"aws_ec2_vpc": true, "aws_s3_bucket": true, "aws_resource": true}
	if len(values) != len(want) {
		t.Fatalf("DeclaredResourceTypeValues() = %v, want %d values", values, len(want))
	}
	for _, v := range values {
		if !want[v] {
			t.Errorf("unexpected declared value %q", v)
		}
	}
}

// TestKnownTargetTypesUnionIncludesAllowlist proves the known set is the union
// of declared constants and the documented allowlist, so a forward-reference
// target type is accepted while still being explicit.
func TestKnownTargetTypesUnionIncludesAllowlist(t *testing.T) {
	awscloudDir, _ := writeFixtureTree(t, fixtureConstants, nil)
	known, err := relguard.KnownTargetTypes(awscloudDir)
	if err != nil {
		t.Fatalf("KnownTargetTypes() error = %v", err)
	}
	if _, ok := known["aws_ec2_vpc"]; !ok {
		t.Errorf("known set missing declared constant value aws_ec2_vpc")
	}
	for allow := range relguard.KnownTargetTypeAllowlist {
		if _, ok := known[allow]; !ok {
			t.Errorf("known set missing allowlist entry %q", allow)
		}
	}
}

// TestEmittedResolvesLiteralConstAndLocal proves the AST walk resolves the three
// statically determinable shapes: an inline literal, a package const, and a
// file-local single-literal assignment.
func TestEmittedResolvesLiteralConstAndLocal(t *testing.T) {
	src := relationshipsHeader + `
const localConst = "aws_s3_bucket"

func inlineLiteral() RelationshipObservation {
	return RelationshipObservation{RelationshipType: "r1", TargetType: "aws_ec2_vpc"}
}

func packageConst() RelationshipObservation {
	return RelationshipObservation{RelationshipType: "r2", TargetType: localConst}
}

func localAssign() RelationshipObservation {
	tt := "aws_resource"
	return RelationshipObservation{RelationshipType: "r3", TargetType: tt}
}
`
	_, servicesDir := writeFixtureTree(t, fixtureConstants, map[string]string{"svc": src})
	literals, unresolved, err := relguard.EmittedTargetTypeLiterals(servicesDir)
	if err != nil {
		t.Fatalf("EmittedTargetTypeLiterals() error = %v", err)
	}
	if unresolved != 0 {
		t.Errorf("unresolved = %d, want 0 for all-static fixture", unresolved)
	}
	got := map[string]bool{}
	for _, lit := range literals {
		got[lit.Value] = true
	}
	for _, want := range []string{"aws_ec2_vpc", "aws_s3_bucket", "aws_resource"} {
		if !got[want] {
			t.Errorf("expected resolved literal %q, got %v", want, literals)
		}
	}
}

// TestEmittedSkipsHelperAndConstBacked proves a helper-call target is left
// unresolved (handed to the runtime layer) and an awscloud.ResourceType*
// selector is recorded as const-backed, not as an unknown literal.
func TestEmittedSkipsHelperAndConstBacked(t *testing.T) {
	src := relationshipsHeaderWithImport + `
func helperTarget(s string) RelationshipObservation {
	return RelationshipObservation{RelationshipType: "r1", TargetType: classify(s)}
}

func constBacked() RelationshipObservation {
	return RelationshipObservation{RelationshipType: "r2", TargetType: awscloud.ResourceTypeS3Bucket}
}

func classify(s string) string { return s }
`
	_, servicesDir := writeFixtureTree(t, fixtureConstants, map[string]string{"svc": src})
	literals, unresolved, err := relguard.EmittedTargetTypeLiterals(servicesDir)
	if err != nil {
		t.Fatalf("EmittedTargetTypeLiterals() error = %v", err)
	}
	if unresolved != 1 {
		t.Errorf("unresolved = %d, want 1 (the helper call)", unresolved)
	}
	constBacked := 0
	for _, lit := range literals {
		if lit.ConstBacked {
			constBacked++
		}
	}
	if constBacked != 1 {
		t.Errorf("const-backed = %d, want 1 (the awscloud.ResourceType* selector)", constBacked)
	}
}

// TestValidateFlagsEmptyAndUnknown is the static NEGATIVE proof: the guard MUST
// fail when a relationship is given an empty or unknown target_type. Without
// this proof the guard could silently pass on the exact defect class issue #804
// exists to catch.
func TestValidateFlagsEmptyAndUnknown(t *testing.T) {
	src := relationshipsHeader + `
func emptyTarget() RelationshipObservation {
	return RelationshipObservation{RelationshipType: "empty", TargetType: ""}
}

func unknownTarget() RelationshipObservation {
	return RelationshipObservation{RelationshipType: "unknown", TargetType: "aws_typo_resource"}
}

func goodTarget() RelationshipObservation {
	return RelationshipObservation{RelationshipType: "good", TargetType: "aws_ec2_vpc"}
}
`
	awscloudDir, servicesDir := writeFixtureTree(t, fixtureConstants, map[string]string{"svc": src})
	resolved, _, err := relguard.ValidateEmitted(awscloudDir, servicesDir)
	if err == nil {
		t.Fatalf("ValidateEmitted() = nil error, want a violation for the empty and unknown target_type")
	}
	msg := err.Error()
	if !strings.Contains(msg, "aws_typo_resource") {
		t.Errorf("error %q does not name the unknown target_type", msg)
	}
	if !strings.Contains(msg, "empty target_type") {
		t.Errorf("error %q does not flag the empty target_type", msg)
	}
	if resolved == 0 {
		t.Errorf("resolved = 0, want the fixture literals to have been walked")
	}
}

// TestValidatePassesWhenAllKnown proves the guard does not false-positive on a
// fully valid fixture: an inline declared value plus an allowlisted value.
func TestValidatePassesWhenAllKnown(t *testing.T) {
	src := relationshipsHeader + `
func declared() RelationshipObservation {
	return RelationshipObservation{RelationshipType: "d", TargetType: "aws_ec2_vpc"}
}

func allowlisted() RelationshipObservation {
	return RelationshipObservation{RelationshipType: "a", TargetType: "aws_resource"}
}
`
	awscloudDir, servicesDir := writeFixtureTree(t, fixtureConstants, map[string]string{"svc": src})
	if _, _, err := relguard.ValidateEmitted(awscloudDir, servicesDir); err != nil {
		t.Fatalf("ValidateEmitted() = %v, want nil for an all-known fixture", err)
	}
}

// recordingTB captures Errorf calls so the runtime-layer negative proofs can
// assert AssertObservations fails on a bad edge.
type recordingTB struct {
	errors []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

// TestRuntimeCheckCatchesDataDependentDefects is the runtime NEGATIVE proof. It
// covers the cases the static layer cannot see because a helper or field read
// produced the value: empty target_type, unknown target_type, and an ARN-typed
// target keyed by a bare name instead of the ARN.
func TestRuntimeCheckCatchesDataDependentDefects(t *testing.T) {
	known, err := relguard.KnownTargetTypeSet()
	if err != nil {
		t.Fatalf("KnownTargetTypeSet() error = %v", err)
	}
	cases := []struct {
		name string
		obs  awscloud.RelationshipObservation
		want string
	}{
		{
			name: "empty target_type",
			obs: awscloud.RelationshipObservation{
				RelationshipType: "x_uses_y", TargetResourceID: "id", TargetType: "",
			},
			want: "empty target_type",
		},
		{
			name: "unknown target_type",
			obs: awscloud.RelationshipObservation{
				RelationshipType: "x_uses_y", TargetResourceID: "id", TargetType: "aws_not_a_thing",
			},
			want: "unknown target_type",
		},
		{
			name: "ARN-keyed target keyed by bare name",
			obs: awscloud.RelationshipObservation{
				RelationshipType: "x_uses_y",
				TargetResourceID: "my-bucket",
				TargetARN:        "arn:aws:s3:::my-bucket",
				TargetType:       "aws_s3_bucket",
			},
			want: "not an ARN",
		},
		{
			name: "malformed target_arn",
			obs: awscloud.RelationshipObservation{
				RelationshipType: "x_uses_y",
				TargetResourceID: "arn:aws:s3:::my-bucket",
				TargetARN:        "my-bucket",
				TargetType:       "aws_s3_bucket",
			},
			want: "not ARN-shaped",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations := relguard.Check(known, tc.obs)
			if len(violations) == 0 {
				t.Fatalf("Check() found no violation, want one mentioning %q", tc.want)
			}
			if !violationMentions(violations, tc.want) {
				t.Fatalf("Check() = %v, want a violation mentioning %q", violations, tc.want)
			}
		})
	}
}

// TestAssertObservationsPassesValidEdge proves the runtime helper is a no-op for
// a correct ARN-keyed edge, so it does not false-fail real scanner tests.
func TestAssertObservationsPassesValidEdge(t *testing.T) {
	rec := &recordingTB{}
	relguard.AssertObservations(rec, awscloud.RelationshipObservation{
		RelationshipType: "x_uses_y",
		TargetResourceID: "arn:aws:s3:::my-bucket",
		TargetARN:        "arn:aws:s3:::my-bucket",
		TargetType:       "aws_s3_bucket",
	})
	if len(rec.errors) != 0 {
		t.Fatalf("AssertObservations flagged a valid edge: %v", rec.errors)
	}
}

// TestAssertObservationsFailsBadEdge proves the runtime helper reports through
// the TB surface a scanner test would use.
func TestAssertObservationsFailsBadEdge(t *testing.T) {
	rec := &recordingTB{}
	relguard.AssertObservations(rec, awscloud.RelationshipObservation{
		RelationshipType: "x_uses_y",
		TargetResourceID: "id",
		TargetType:       "aws_not_a_thing",
	})
	if len(rec.errors) == 0 {
		t.Fatal("AssertObservations did not flag an unknown target_type edge")
	}
}

// TestLiveScannerTreeHasNoGraphJoinDefects is the repo-level guard. It walks the
// real awscloud scanner tree and asserts every statically resolvable target_type
// is non-empty and known. A new scanner that ships an empty or unknown literal
// target_type fails here mechanically, which is the whole point of #804.
func TestLiveScannerTreeHasNoGraphJoinDefects(t *testing.T) {
	awscloudDir := liveAWSCloudDir(t)
	servicesDir := filepath.Join(awscloudDir, "services")
	resolved, unresolved, err := relguard.ValidateEmitted(awscloudDir, servicesDir)
	if err != nil {
		t.Fatalf("live scanner tree has graph-join target_type defects:\n%v", err)
	}
	if resolved == 0 {
		t.Fatal("guard resolved zero target_type literals; the walk did not see the live scanner tree")
	}
	// The fleet mixes static literals and data-dependent helpers; both must be
	// present for the two-layer guard to be meaningful. This asserts the walk is
	// actually exercising real input rather than silently passing.
	t.Logf("relguard static layer: %d resolved literals, %d runtime-only (data-dependent) target types", resolved, unresolved)
}

// liveAWSCloudDir resolves go/internal/collector/awscloud from this test file:
// relguard_test.go -> relguard -> internal -> awscloud.
func liveAWSCloudDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..")
}

func violationMentions(violations []relguard.Violation, substr string) bool {
	for _, v := range violations {
		if strings.Contains(v.Error(), substr) {
			return true
		}
	}
	return false
}
