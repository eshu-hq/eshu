// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudformation

import "testing"

func TestParseWithPositionsStampsRealPerEntityLines(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Parameters": map[string]any{
			"Env": map[string]any{"Default": "dev"},
		},
		"Resources": map[string]any{
			"Bucket": map[string]any{"Type": "AWS::S3::Bucket"},
		},
		"Outputs": map[string]any{
			"BucketArn": map[string]any{
				"Value":  "arn",
				"Export": map[string]any{"Name": "Stack-BucketArn"},
			},
		},
	}

	positions := Positions{
		Parameters: SectionPositions{Entries: map[string]EntityPosition{"Env": {StartLine: 5, EndLine: 7}}},
		Resources:  SectionPositions{Entries: map[string]EntityPosition{"Bucket": {StartLine: 10, EndLine: 12}}},
		Outputs:    SectionPositions{Entries: map[string]EntityPosition{"BucketArn": {StartLine: 20, EndLine: 24}}},
	}

	result := ParseWithPositions(document, "/test/stack.yaml", 1, "yaml", positions)

	if got, want := result.Params[0]["line_number"], 5; got != want {
		t.Fatalf("param line_number = %#v, want %d", got, want)
	}
	if got, want := result.Params[0]["end_line"], 7; got != want {
		t.Fatalf("param end_line = %#v, want %d", got, want)
	}
	if got, want := result.Resources[0]["line_number"], 10; got != want {
		t.Fatalf("resource line_number = %#v, want %d", got, want)
	}
	if got, want := result.Resources[0]["end_line"], 12; got != want {
		t.Fatalf("resource end_line = %#v, want %d", got, want)
	}
	if got, want := result.Outputs[0]["line_number"], 20; got != want {
		t.Fatalf("output line_number = %#v, want %d", got, want)
	}
	if got, want := result.Outputs[0]["end_line"], 24; got != want {
		t.Fatalf("output end_line = %#v, want %d", got, want)
	}
	// An Export inherits its owning Output's own EntityPosition rather than
	// getting a separately-walked line.
	if len(result.Exports) != 1 {
		t.Fatalf("len(result.Exports) = %d, want 1", len(result.Exports))
	}
	if got, want := result.Exports[0]["line_number"], 20; got != want {
		t.Fatalf("export line_number = %#v, want %d", got, want)
	}
	if got, want := result.Exports[0]["end_line"], 24; got != want {
		t.Fatalf("export end_line = %#v, want %d", got, want)
	}
}

func TestParseWithPositionsFallsBackToSectionHeaderLineWhenEntityMissing(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Resources": map[string]any{
			"Bucket": map[string]any{"Type": "AWS::S3::Bucket"},
		},
	}

	// Bucket is absent from Entries (simulating a structural gap the caller's
	// walk could not resolve), but FallbackLine (the section header's own
	// line) is known.
	positions := Positions{
		Resources: SectionPositions{Entries: map[string]EntityPosition{}, FallbackLine: 9},
	}

	result := ParseWithPositions(document, "/test/stack.yaml", 1, "yaml", positions)

	if got, want := result.Resources[0]["line_number"], 9; got != want {
		t.Fatalf("resource line_number = %#v, want %d (section header fallback)", got, want)
	}
	if got, want := result.Resources[0]["end_line"], 9; got != want {
		t.Fatalf("resource end_line = %#v, want %d (section header fallback)", got, want)
	}
}

func TestParseWithPositionsZeroPositionsMatchesParseExactly(t *testing.T) {
	t.Parallel()

	document := map[string]any{
		"AWSTemplateFormatVersion": "2010-09-09",
		"Parameters": map[string]any{
			"Env": map[string]any{"Default": "dev"},
		},
		"Resources": map[string]any{
			"Bucket": map[string]any{"Type": "AWS::S3::Bucket"},
		},
		"Outputs": map[string]any{
			"BucketArn": map[string]any{"Value": "arn"},
		},
	}

	viaParse := Parse(document, "/test/stack.json", 1, "json")
	viaParseWithPositions := ParseWithPositions(document, "/test/stack.json", 1, "json", Positions{})

	for _, name := range []string{"line_number", "end_line"} {
		if got, want := viaParse.Params[0][name], viaParseWithPositions.Params[0][name]; got != want {
			t.Fatalf("Params[0][%q] = %#v, ParseWithPositions gave %#v", name, got, want)
		}
		if got, want := viaParse.Resources[0][name], viaParseWithPositions.Resources[0][name]; got != want {
			t.Fatalf("Resources[0][%q] = %#v, ParseWithPositions gave %#v", name, got, want)
		}
		if got, want := viaParse.Outputs[0][name], viaParseWithPositions.Outputs[0][name]; got != want {
			t.Fatalf("Outputs[0][%q] = %#v, ParseWithPositions gave %#v", name, got, want)
		}
	}
	// Parse's original contract never sets end_line at all -- confirm the
	// zero-Positions path preserves that (not merely "same value").
	if _, ok := viaParse.Params[0]["end_line"]; ok {
		t.Fatalf("Parse() Params[0] has end_line = %#v, want no end_line field", viaParse.Params[0]["end_line"])
	}
	if _, ok := viaParseWithPositions.Params[0]["end_line"]; ok {
		t.Fatalf("ParseWithPositions() with zero Positions has end_line = %#v, want no end_line field", viaParseWithPositions.Params[0]["end_line"])
	}
}
