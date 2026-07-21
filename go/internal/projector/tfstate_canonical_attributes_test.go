// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import "testing"

// TestTerraformStateResourceAttributesUnwrapsSingleKeyRemainder proves the
// #5441 workaround for the factschema decode-plan defect (#5522): a
// single-key remainder map holding {"attributes": {...}} unwraps to the
// inner classified map.
func TestTerraformStateResourceAttributesUnwrapsSingleKeyRemainder(t *testing.T) {
	t.Parallel()

	got := terraformStateResourceAttributes(map[string]any{
		"attributes": map[string]any{"instance_type": "t3.micro"},
	})
	if got["instance_type"] != "t3.micro" {
		t.Fatalf("got %#v, want the unwrapped classified map", got)
	}
	if _, ok := got["attributes"]; ok {
		t.Fatalf("wrapper key survived unwrap: %#v", got)
	}
}

// TestTerraformStateResourceAttributesMultiKeyRemainderDegradesToRawValue is
// the P2 finding F5 regression guard: if a future factschema change (see
// #5522) adds a second unclaimed field alongside "attributes", the
// len(attributes) != 1 guard short-circuits and returns the RAW,
// still-self-nested remainder map unmodified. terraformAttributePathValue
// then finds nothing at the remainder's top level (the real classified
// attributes stay nested one level under "attributes"), so promotion
// silently yields zero properties instead of erroring or fabricating data.
// This test locks in that this is the current, deliberate, safe-direction
// degrade (drop, never corrupt) so a change to this guard is a conscious
// decision caught here, not a silent regression first noticed in
// production.
func TestTerraformStateResourceAttributesMultiKeyRemainderDegradesToRawValue(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"attributes":      map[string]any{"instance_type": "t3.micro"},
		"unclaimed_field": "some-future-field",
	}
	got := terraformStateResourceAttributes(input)

	if _, ok := got["instance_type"]; ok {
		t.Fatalf("multi-key remainder was incorrectly unwrapped: %#v", got)
	}
	wrapped, ok := got["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("expected the raw self-nested remainder to pass through unmodified, got %#v", got)
	}
	if wrapped["instance_type"] != "t3.micro" {
		t.Fatalf("raw remainder lost its nested classified data: %#v", got)
	}
}

// TestTerraformStateResourceAttributesPassesThroughNonWrapperShapes covers
// the remaining guard branches: nil input, and a single key that is not
// literally "attributes" (already-flat data, which must pass through
// unmodified rather than being misdetected as a wrapper).
func TestTerraformStateResourceAttributesPassesThroughNonWrapperShapes(t *testing.T) {
	t.Parallel()

	if got := terraformStateResourceAttributes(nil); got != nil {
		t.Fatalf("nil input = %#v, want nil", got)
	}

	alreadyFlat := map[string]any{"arn": "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc"}
	got := terraformStateResourceAttributes(alreadyFlat)
	if got["arn"] != "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc" {
		t.Fatalf("already-flat single-key map was altered: %#v", got)
	}

	wrongValueType := map[string]any{"attributes": "not-a-map"}
	got = terraformStateResourceAttributes(wrongValueType)
	if got["attributes"] != "not-a-map" {
		t.Fatalf("non-map \"attributes\" value should pass through unmodified: %#v", got)
	}
}
