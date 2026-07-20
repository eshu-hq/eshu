// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathYAMLCrossplaneResources is split out of
// engine_yaml_semantics_test.go (issue #5347) to keep that file under the
// repo's 500-line package-file cap.
func TestDefaultEngineParsePathYAMLCrossplaneResources(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "crossplane.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xiamroles.iam.aws.myorg.io
spec:
  group: iam.aws.myorg.io
  names:
    kind: XIAMRole
    plural: xiamroles
  claimNames:
    kind: IAMRole
    plural: iamroles
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: iam-role-composition
spec:
  compositeTypeRef:
    apiVersion: iam.aws.myorg.io/v1alpha1
    kind: XIAMRole
  resources:
    - name: iam-role
---
apiVersion: iam.aws.myorg.io/v1alpha1
kind: IAMRole
metadata:
  name: my-service-role
  namespace: default
---
apiVersion: ec2.aws.crossplane.io/v1alpha1
kind: Instance
metadata:
  name: my-managed-instance
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "crossplane_xrds", "xiamroles.iam.aws.myorg.io")
	assertBucketContainsFieldValue(t, got, "crossplane_xrds", "claim_kind", "IAMRole")
	assertNamedBucketContains(t, got, "crossplane_compositions", "iam-role-composition")
	assertBucketContainsFieldValue(t, got, "crossplane_compositions", "composite_kind", "XIAMRole")

	// A Crossplane Claim uses the XRD's own custom group
	// (spec.group, here iam.aws.myorg.io) — never a *.crossplane.io/ apiVersion
	// (that substring belongs to provider Managed Resources and Crossplane's
	// own apiextensions/pkg groups). It carries no dedicated bucket: the
	// object stays a generic k8s_resources row (matching parseK8sResource's
	// api_version/kind join keys) and the reducer correlation layer
	// classifies it against crossplane_xrds by (group, kind) ==
	// (spec.group, spec.claimNames.kind), materializing a SATISFIED_BY edge
	// rather than a parse-time label (issue #5347).
	assertNamedBucketContains(t, got, "k8s_resources", "my-service-role")
	assertBucketContainsFieldValue(t, got, "k8s_resources", "api_version", "iam.aws.myorg.io/v1alpha1")
	assertBucketContainsFieldValue(t, got, "k8s_resources", "kind", "IAMRole")

	// A provider Managed Resource (e.g. ec2.aws.crossplane.io/v1alpha1) DOES
	// contain the ".crossplane.io/" substring the old isCrossplaneClaim keyed
	// on — that inverted heuristic misclassified exactly this kind of
	// document as a "claim" while missing every real Claim above. It must
	// flow to k8s_resources like any other generic Kubernetes object, never
	// into a Crossplane-specific bucket.
	assertNamedBucketContains(t, got, "k8s_resources", "my-managed-instance")
	assertBucketContainsFieldValue(t, got, "k8s_resources", "api_version", "ec2.aws.crossplane.io/v1alpha1")

	for _, row := range got["crossplane_claims"].([]map[string]any) {
		t.Errorf("crossplane_claims bucket must stay empty (Claims are edge-only, #5347), got row %#v", row)
	}
}
