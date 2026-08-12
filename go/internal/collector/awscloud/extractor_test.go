// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The registry is the convergence point issue #4591 asks awscloud to adopt from
// gcpcloud: one extractor per resource type, registered from that type's own
// file, so parallel type additions never collide in a shared parser switch.
// These tests pin the contract before any scanner migrates onto it.

func TestRegisterResourceExtractorRoundTrips(t *testing.T) {
	const resourceType = "AWS::Test::RoundTrip"
	t.Cleanup(func() { unregisterResourceExtractorForTest(resourceType) })

	want := AttributeExtraction{
		Attributes:         map[string]any{"engine": "postgres"},
		CorrelationAnchors: []string{"arn:aws:kms:us-east-1:000000000000:key/test"},
	}
	RegisterResourceExtractor(resourceType, func(ExtractContext) (AttributeExtraction, error) {
		return want, nil
	})

	if !HasResourceExtractor(resourceType) {
		t.Fatalf("HasResourceExtractor(%q) = false, want true", resourceType)
	}

	got, handled, err := extractResourceAttributes(ExtractContext{ResourceType: resourceType})
	if err != nil {
		t.Fatalf("extractResourceAttributes() error = %v", err)
	}
	if !handled {
		t.Fatal("extractResourceAttributes() handled = false, want true")
	}
	if got.Attributes["engine"] != "postgres" {
		t.Fatalf("Attributes[engine] = %v, want postgres", got.Attributes["engine"])
	}
	if len(got.CorrelationAnchors) != 1 {
		t.Fatalf("CorrelationAnchors len = %d, want 1", len(got.CorrelationAnchors))
	}
}

// An unregistered type must be handled=false with no error, so the parser keeps
// emitting its bounded base observation for types that carry no typed depth.
// This is what makes the migration incremental: unmigrated scanners are
// unaffected.
func TestExtractResourceAttributesUnregisteredIsNotAnError(t *testing.T) {
	got, handled, err := extractResourceAttributes(ExtractContext{ResourceType: "AWS::Test::NeverRegistered"})
	if err != nil {
		t.Fatalf("extractResourceAttributes() error = %v, want nil", err)
	}
	if handled {
		t.Fatal("extractResourceAttributes() handled = true, want false")
	}
	if got.Attributes != nil || got.CorrelationAnchors != nil || got.Relationships != nil {
		t.Fatalf("extractResourceAttributes() = %+v, want zero value", got)
	}
}

// An extractor error is attributed to its resource type without leaking the
// resource data that produced it.
func TestExtractResourceAttributesWrapsExtractorError(t *testing.T) {
	const resourceType = "AWS::Test::Failing"
	t.Cleanup(func() { unregisterResourceExtractorForTest(resourceType) })

	sentinel := errors.New("boom")
	RegisterResourceExtractor(resourceType, func(ExtractContext) (AttributeExtraction, error) {
		return AttributeExtraction{}, sentinel
	})

	_, handled, err := extractResourceAttributes(ExtractContext{
		ResourceType: resourceType,
		Data:         json.RawMessage(`{"MasterUserPassword":"hunter2"}`),
	})
	if !handled {
		t.Fatal("handled = false, want true for a registered type that errored")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want wrapping of %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), resourceType) {
		t.Fatalf("error %q does not name the resource type", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error %q leaked resource data", err)
	}
}

// Lookup tolerates surrounding whitespace on both registration and lookup, so a
// stray space in a constant cannot silently disable an extractor.
func TestResourceExtractorLookupTrimsWhitespace(t *testing.T) {
	const resourceType = "AWS::Test::Trimmed"
	t.Cleanup(func() { unregisterResourceExtractorForTest(resourceType) })

	RegisterResourceExtractor("  "+resourceType+"  ", func(ExtractContext) (AttributeExtraction, error) {
		return AttributeExtraction{}, nil
	})
	if !HasResourceExtractor(resourceType) {
		t.Fatalf("HasResourceExtractor(%q) = false after padded registration", resourceType)
	}
}

// Wiring mistakes fail loudly at init rather than silently dropping typed depth
// or shadowing another type's extractor.
func TestRegisterResourceExtractorPanicsOnWiringMistakes(t *testing.T) {
	valid := func(ExtractContext) (AttributeExtraction, error) { return AttributeExtraction{}, nil }

	t.Run("blank resource type", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on blank resource type")
			}
		}()
		RegisterResourceExtractor("   ", valid)
	})

	t.Run("nil extractor", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on nil extractor")
			}
		}()
		RegisterResourceExtractor("AWS::Test::NilExtractor", nil)
	})

	t.Run("duplicate registration", func(t *testing.T) {
		const resourceType = "AWS::Test::Duplicate"
		t.Cleanup(func() { unregisterResourceExtractorForTest(resourceType) })
		RegisterResourceExtractor(resourceType, valid)
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on duplicate registration")
			}
		}()
		RegisterResourceExtractor(resourceType, valid)
	})
}
