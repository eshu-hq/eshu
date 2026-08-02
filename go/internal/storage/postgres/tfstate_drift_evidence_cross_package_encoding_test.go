// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/hcl"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// This file is the cross-package regression AGENTS.md:389-401 requires for
// any change to the state-side dot-path encoding rules: "flattenStateAttributes
// must stay byte-identical to ctyValueToDriftString in
// go/internal/parser/hcl/terraform_resource_attributes.go; the classifier's
// value-equality check depends on both sides agreeing at the leaf level."
//
// #5859 added a new encoding rule to flattenStateAttributes (a redaction
// marker map is treated as absent rather than recursed into) without one.
// Before this file, nothing in the repo called both the parser's Parse and
// the storage package's flattenStateAttributes on the same input and diffed
// the output -- the invariant was documented but never proven, so the two
// encoders could drift apart with every existing test staying green (see
// TestCrossPackageEncodingByteIdenticalForSharedScalarLeaves's neuter proof
// in its own doc comment).
//
// ctyValueToDriftString and literalAttributeValue are unexported in package
// hcl, so this uses hcl.Parse -- the parser's real, public entry point --
// rather than reaching for a copy of the formatting rule. That keeps the
// comparison honest: it exercises the exact function production code calls
// (configRowFromParserEntry copies "attributes" from this same JSON shape
// onto ResourceRow.Attributes), not a hand-maintained mirror of it.

// writeCrossPackageHCLFixture writes body to a temp .tf file and returns its
// path, for feeding into hcl.Parse.
func writeCrossPackageHCLFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.tf")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write HCL fixture: %v", err)
	}
	return path
}

// parserConfigAttributesForTest parses one HCL fixture with the parser's
// real public entry point and returns the named resource's dot-path
// "attributes" map -- the exact shape configRowFromParserEntry copies onto
// ResourceRow.Attributes on the config side of the classifier.
func parserConfigAttributesForTest(t *testing.T, path, resourceName string) map[string]any {
	t.Helper()
	got, err := hcl.Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatalf("hcl.Parse(%q) error = %v, want nil", path, err)
	}
	resources, ok := got["terraform_resources"].([]map[string]any)
	if !ok {
		t.Fatalf("terraform_resources = %T, want []map[string]any", got["terraform_resources"])
	}
	for _, row := range resources {
		if row["name"] == resourceName {
			attrs, ok := row["attributes"].(map[string]any)
			if !ok {
				t.Fatalf("attributes for %q = %T, want map[string]any", resourceName, row["attributes"])
			}
			return attrs
		}
	}
	t.Fatalf("resource %q not found in parsed resources %#v", resourceName, resources)
	return nil
}

// stateAttributesForTest decodes a terraform_state_resource-shaped attributes
// JSON object and runs it through the real flattenStateAttributes, mirroring
// what stateRowFromCollectorPayload does for a genuine collector payload.
func stateAttributesForTest(t *testing.T, attributesJSON string) map[string]string {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(attributesJSON), &decoded); err != nil {
		t.Fatalf("unmarshal state attributes fixture: %v", err)
	}
	out := map[string]string{}
	flattenStateAttributes(context.Background(), nil, decoded, "", out)
	return out
}

// TestCrossPackageEncodingByteIdenticalForSharedScalarLeaves is the
// AGENTS.md:389-401 cross-package regression: it runs the SAME logical
// resource through the real parser (hcl.Parse) and the real state flattener
// (flattenStateAttributes) and asserts every dot-path leaf they both produce
// is byte-identical, not merely deep-equal. classifyAttributeDrift
// (go/internal/correlation/drift/tfconfigstate/classify.go:215) compares
// cfgValue != stateValue with plain Go string inequality, so a single
// formatting difference on a shared leaf -- a trailing zero, a case
// difference, a quoting difference -- makes two equal declared/observed
// values classify as attribute_drift, or (less obviously) can round-trip an
// actually-different pair back to looking equal.
//
// Proof that this test would catch a real divergence: change
// coerceJSONString's bool branch (tfstate_drift_evidence_helpers.go) to
// return "True"/"False" instead of "true"/"false" -- go test ./internal/storage/postgres/...
// -run TestCrossPackageEncodingByteIdenticalForSharedScalarLeaves fails on
// the "monitoring" leaf ("True" != "true" from ctyValueToDriftString) -- then
// revert and confirm `shasum -a 256` on the file is unchanged from before
// the edit.
func TestCrossPackageEncodingByteIdenticalForSharedScalarLeaves(t *testing.T) {
	t.Parallel()

	// One scalar of each kind ctyValueToDriftString formats (string, bool,
	// integer) plus one singleton nested block, so both the top-level and the
	// recursive/singleton-array-unwrap paths of flattenStateAttributes are
	// exercised against the parser side. volume_size stays well under the
	// 1e6 threshold coerceJSONString's doc comment already flags as a known,
	// separately-tracked divergence for any FUTURE numeric allowlist entry
	// that could reach it (fmt.Sprint(float64) switches to scientific
	// notation there while strconv.FormatInt never does) -- reproducing that
	// pre-existing, documented gap is not this test's job.
	const resourceName = "aws_instance.web"
	hclBody := `resource "aws_instance" "web" {
  instance_type = "t3.micro"
  monitoring    = true
  ami           = "ami-0123456789abcdef0"
  ebs_block_device {
    volume_size = 100
  }
}
`
	fixturePath := writeCrossPackageHCLFixture(t, hclBody)
	configAttrs := parserConfigAttributesForTest(t, fixturePath, resourceName)

	stateAttrs := stateAttributesForTest(t, `{
		"instance_type": "t3.micro",
		"monitoring": true,
		"ami": "ami-0123456789abcdef0",
		"ebs_block_device": [{"volume_size": 100}]
	}`)

	wantKeys := []string{"instance_type", "monitoring", "ami", "ebs_block_device.volume_size"}
	for _, key := range wantKeys {
		cfgValue, cfgHas := configAttrs[key]
		stateValue, stateHas := stateAttrs[key]
		if !cfgHas || !stateHas {
			t.Fatalf("key %q: cfgHas=%v stateHas=%v, want both true (config=%#v, state=%#v)",
				key, cfgHas, stateHas, configAttrs, stateAttrs)
		}
		cfgString, ok := cfgValue.(string)
		if !ok {
			t.Fatalf("config attribute %q = %T, want string (ctyValueToDriftString always returns a string)", key, cfgValue)
		}
		if cfgString != stateValue {
			t.Fatalf("cross-package encoding diverged for %q: parser (ctyValueToDriftString) = %q, "+
				"state (flattenStateAttributes/coerceJSONString) = %q -- classifyAttributeDrift's "+
				"cfgValue != stateValue check would misfire on this leaf", key, cfgString, stateValue)
		}
	}

	if len(configAttrs) != len(wantKeys) {
		t.Fatalf("config attributes = %#v, want exactly %v (unexpected extra/missing parser-side keys)", configAttrs, wantKeys)
	}
	if len(stateAttrs) != len(wantKeys) {
		t.Fatalf("state attributes = %#v, want exactly %v (unexpected extra/missing state-side keys)", stateAttrs, wantKeys)
	}
}

// TestCrossPackageEncodingRedactionMarkerOmitsKeyOnStateSideOnly is the
// marker-specific half of the same invariant: a redaction marker can only
// ever appear on the state side (it is a runtime collector artifact, not
// something Terraform HCL source can express), so the "byte-identical"
// contract for a redacted leaf is that flattenStateAttributes omits the key
// entirely while the parser side -- given a REAL literal for the same
// attribute, since that is the only thing HCL can produce -- still emits it.
//
// This is safe by construction, not by accident: classifyAttributeDrift only
// compares an allowlisted attribute when BOTH config and state have the key
// (`if !cfgHas || !stateHas { continue }`, classify.go:212). Proving state
// omits "ami" here, alongside the sibling test proving config still has real
// values for its own leaves, pins that the marker early-return
// (tfstate_drift_evidence_state_row.go:108) cannot make an unrelated leaf
// silently start comparing wrong -- it can only ever suppress the one key it
// targets.
func TestCrossPackageEncodingRedactionMarkerOmitsKeyOnStateSideOnly(t *testing.T) {
	t.Parallel()

	const resourceName = "aws_instance.web"
	hclBody := `resource "aws_instance" "web" {
  instance_type = "t3.micro"
  ami           = "ami-0123456789abcdef0"
}
`
	fixturePath := writeCrossPackageHCLFixture(t, hclBody)
	configAttrs := parserConfigAttributesForTest(t, fixturePath, resourceName)
	if _, ok := configAttrs["ami"].(string); !ok {
		t.Fatalf("config attributes = %#v, want a real string \"ami\" leaf (HCL cannot express a redaction marker)", configAttrs)
	}

	stateAttrs := stateAttributesForTest(t, `{
		"instance_type": "t3.micro",
		"ami": {
			"marker": "redacted:hmac-sha256:`+repeat64Zero+`",
			"reason": "unknown_provider_schema",
			"source": "resources.*.attributes.ami"
		}
	}`)
	if _, present := stateAttrs["ami"]; present {
		t.Fatalf("state attributes = %#v, want no \"ami\" key: a redaction marker must not survive as a comparable leaf, "+
			"and classifyAttributeDrift only fires when both config AND state carry the key", stateAttrs)
	}
	if stateAttrs["instance_type"] != configAttrs["instance_type"] {
		t.Fatalf("instance_type diverged: config = %v, state = %q; redacting a sibling key must not disturb this leaf",
			configAttrs["instance_type"], stateAttrs["instance_type"])
	}
}

const repeat64Zero = "0000000000000000000000000000000000000000000000000000000000000000"
