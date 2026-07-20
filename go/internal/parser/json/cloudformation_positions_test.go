// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	stdjson "encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/cloudformation"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// cfnPositionsFromSource builds the exact (normalizedBytes, translated
// newlineIndex, flattened document) triple Parse's CloudFormation branch uses
// and returns the resolved Positions and fallbacks. It exercises the real
// normalizeJSONSource + buildTranslatedNewlineIndex pairing so a test cannot
// pass against a wrong (untranslated) index (issue #5348 Q4).
func cfnPositionsFromSource(t *testing.T, src string) (cloudformation.Positions, []cloudformationPositionFallback, map[string]any) {
	t.Helper()

	normalized, translate := normalizeJSONSource([]byte(src), "template.json")
	normalizedBytes := []byte(normalized)
	var document any
	if err := stdjson.Unmarshal(normalizedBytes, &document); err != nil {
		t.Fatalf("stdjson.Unmarshal() error = %v, want nil", err)
	}
	object, ok := document.(map[string]any)
	if !ok {
		t.Fatalf("document = %T, want map[string]any", document)
	}
	idx := buildTranslatedNewlineIndex([]byte(src), translate)
	positions, fallbacks := cloudformationPositionsFromDocument(normalizedBytes, idx, object)
	return positions, fallbacks, object
}

func assertEntityPosition(t *testing.T, section cloudformation.SectionPositions, name string, wantStart, wantEnd int) {
	t.Helper()

	pos, ok := section.Entries[name]
	if !ok {
		t.Fatalf("section entry %q missing from %#v", name, section.Entries)
	}
	if pos.StartLine != wantStart {
		t.Fatalf("%q StartLine = %d, want %d", name, pos.StartLine, wantStart)
	}
	if pos.EndLine != wantEnd {
		t.Fatalf("%q EndLine = %d, want %d", name, pos.EndLine, wantEnd)
	}
}

// TestCloudformationPositionsFromDocumentRealLines proves the JSON position
// walk resolves each section entity's own real start line (its key line) and
// end line (the on-disk line of the value's final byte -- the closing brace).
func TestCloudformationPositionsFromDocumentRealLines(t *testing.T) {
	t.Parallel()

	src := `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Parameters": {
    "InstanceType": {
      "Type": "String",
      "Default": "t3.micro"
    }
  },
  "Conditions": {
    "IsProd": {"Fn::Equals": ["a", "b"]}
  },
  "Resources": {
    "WebServer": {
      "Type": "AWS::EC2::Instance",
      "Properties": {
        "ImageId": "ami-123"
      }
    }
  },
  "Outputs": {
    "InstanceId": {
      "Value": {"Ref": "WebServer"}
    }
  }
}`

	positions, fallbacks, _ := cfnPositionsFromSource(t, src)
	if len(fallbacks) != 0 {
		t.Fatalf("fallbacks = %#v, want none on the happy path", fallbacks)
	}

	assertEntityPosition(t, positions.Parameters, "InstanceType", 4, 7)
	assertEntityPosition(t, positions.Conditions, "IsProd", 10, 10)
	assertEntityPosition(t, positions.Resources, "WebServer", 13, 18)
	assertEntityPosition(t, positions.Outputs, "InstanceId", 21, 23)
}

// TestCloudformationPositionsLastWinsDuplicateEntityNames proves a duplicate
// entity name inside a section resolves to the LAST occurrence's position,
// matching stdjson.Unmarshal's last-wins flattening of the map value. A
// first-wins walk would stamp the position of a definition the flattened
// document no longer contains.
func TestCloudformationPositionsLastWinsDuplicateEntityNames(t *testing.T) {
	t.Parallel()

	src := `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Resources": {
    "Bucket": {
      "Type": "AWS::S3::Bucket"
    },
    "Bucket": {
      "Type": "AWS::S3::Bucket",
      "Properties": {
        "BucketName": "second"
      }
    }
  }
}`

	positions, _, object := cfnPositionsFromSource(t, src)

	// stdjson flattening keeps the LAST Bucket (with Properties, closing at
	// line 11). The position walk must agree.
	resources, _ := object["Resources"].(map[string]any)
	if _, ok := resources["Bucket"]; !ok {
		t.Fatalf("flattened Resources missing Bucket: %#v", resources)
	}
	assertEntityPosition(t, positions.Resources, "Bucket", 7, 12)
}

// TestCloudformationPositionsRootAnchoredNestedSameNameKey proves the walk
// anchors strictly at the document root's own top-level section pairs: a
// nested "Resources" key inside a resource's Properties (an
// AWS::CloudFormation::Stack body) is never mistaken for the template's real
// Resources section, and the nested entity name never leaks into Positions.
func TestCloudformationPositionsRootAnchoredNestedSameNameKey(t *testing.T) {
	t.Parallel()

	src := `{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Resources": {
    "NestedStack": {
      "Type": "AWS::CloudFormation::Stack",
      "Properties": {
        "Resources": {
          "FakeInnerBucket": {"Type": "AWS::S3::Bucket"}
        }
      }
    },
    "RealBucket": {
      "Type": "AWS::S3::Bucket"
    }
  }
}`

	positions, _, _ := cfnPositionsFromSource(t, src)

	if _, leaked := positions.Resources.Entries["FakeInnerBucket"]; leaked {
		t.Fatalf("nested FakeInnerBucket leaked into top-level Resources positions: %#v", positions.Resources.Entries)
	}
	if len(positions.Resources.Entries) != 2 {
		t.Fatalf("Resources.Entries = %#v, want exactly NestedStack and RealBucket", positions.Resources.Entries)
	}
	assertEntityPosition(t, positions.Resources, "NestedStack", 4, 11)
	assertEntityPosition(t, positions.Resources, "RealBucket", 12, 14)
}

// TestParseJSONCloudFormationBannerPrefixPreservesRealLines is the issue #5348
// Q4 regression pin: normalizeJSONSource strips a leading `{{ ... }}` banner
// line and leading blank lines before encoding/json sees the template, so the
// CloudFormation position walk must query its newlineIndex through the same
// offset translator the generic JSON path uses. If the adapter fed raw on-disk
// bytes (decode would fail and every entity would silently fall back to line 1)
// or an untranslated index (every line after the stripped prefix would be too
// small), the real on-disk lines below would be wrong. WebServer's key sits on
// real on-disk line 6; end brace on line 8.
func TestParseJSONCloudFormationBannerPrefixPreservesRealLines(t *testing.T) {
	t.Parallel()

	// Line 1: `{{ banner }}`, line 2: blank, then the template from line 3.
	body := "{{ generated banner }}\n\n{\n  \"AWSTemplateFormatVersion\": \"2010-09-09\",\n  \"Resources\": {\n    \"WebServer\": {\n      \"Type\": \"AWS::EC2::Instance\"\n    }\n  }\n}"
	path := writeJSONTestFile(t, "stack.json", body)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows, ok := payload["cloudformation_resources"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("cloudformation_resources = %#v, want exactly one row", payload["cloudformation_resources"])
	}
	row := rows[0]
	if got, want := row["line_number"], 6; got != want {
		t.Fatalf("WebServer line_number = %#v, want %d (real on-disk line, translated past the banner)", got, want)
	}
	if got, want := row["end_line"], 8; got != want {
		t.Fatalf("WebServer end_line = %#v, want %d (real on-disk closing-brace line)", got, want)
	}

	// The banner-prefixed template still decodes cleanly, so no degraded
	// position fallback rows are recorded.
	if fallbacks, ok := payload["cloudformation_position_fallbacks"].([]map[string]any); ok && len(fallbacks) != 0 {
		t.Fatalf("cloudformation_position_fallbacks = %#v, want none", fallbacks)
	}
}

// TestParseJSONCloudFormationMinifiedSingleLine proves a minified single-line
// JSON CloudFormation template resolves every entity to line 1 / end_line 1 --
// correct for its on-disk shape and emitted as a known position (so end_line
// is set), producing zero identity churn on re-index rather than a fabricated
// multi-line span.
func TestParseJSONCloudFormationMinifiedSingleLine(t *testing.T) {
	t.Parallel()

	body := `{"AWSTemplateFormatVersion":"2010-09-09","Resources":{"WebServer":{"Type":"AWS::EC2::Instance"}},"Outputs":{"InstanceId":{"Value":{"Ref":"WebServer"}}}}`
	path := writeJSONTestFile(t, "stack.json", body)

	payload, err := Parse(path, false, shared.Options{}, Config{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	for _, bucket := range []string{"cloudformation_resources", "cloudformation_outputs"} {
		rows, ok := payload[bucket].([]map[string]any)
		if !ok || len(rows) != 1 {
			t.Fatalf("%s = %#v, want exactly one row", bucket, payload[bucket])
		}
		if got, want := rows[0]["line_number"], 1; got != want {
			t.Fatalf("%s line_number = %#v, want %d", bucket, got, want)
		}
		if got, want := rows[0]["end_line"], 1; got != want {
			t.Fatalf("%s end_line = %#v, want %d", bucket, got, want)
		}
	}
}
