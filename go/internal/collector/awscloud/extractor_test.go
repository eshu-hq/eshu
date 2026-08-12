// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
)

// The registry dispatches one extractor per resource type, registered from that
// type's own file, so parallel type additions never collide in a shared parser
// switch. Its producer is the AWS Config lane (#6088), not the service scanners
// — see the header comment in extractor.go for why. These tests pin the contract
// before that lane registers anything.

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
// That is what lets the Config lane add extractors one resource type at a time
// without stranding the types it has not covered yet.
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

// The registry is deliberately empty until AWS Config ingestion exists to feed
// it. That is a comment in extractor.go, and a comment does not fail a build, so
// this asserts it.
//
// A registration landing here before the Config lane means someone migrated a
// service scanner into the registry. That change cannot alter fact output —
// nothing dispatches through resourceExtractors — so its own fixture-parity
// review would pass while the migration did nothing. The scanners already emit
// typed depth through ResourceObservation; see the header comment in
// extractor.go for why they are not this registry's producer.
//
// When the AWS Config lane lands and registers real extractors, delete this
// test in that PR. Changing it deliberately is the point; tripping over it is
// the warning.
func TestResourceExtractorRegistryIsEmptyUntilConfigLaneExists(t *testing.T) {
	if len(resourceExtractors) != 0 {
		registered := make([]string, 0, len(resourceExtractors))
		for resourceType := range resourceExtractors {
			registered = append(registered, resourceType)
		}
		sort.Strings(registered)
		t.Fatalf("resourceExtractors has %d registration(s) %v, want none until the AWS Config "+
			"lane feeds this registry; a scanner migration here changes no fact output",
			len(registered), registered)
	}
}
