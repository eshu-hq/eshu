// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"errors"
	"testing"
)

// testExtractorAssetType is a sentinel asset type used only by registry unit
// tests so they never collide with a production extractor registration.
const testExtractorAssetType = "test.eshu.invalid/Sentinel"

func TestRegisterAssetExtractorRejectsBlankAssetType(t *testing.T) {
	defer expectPanic(t, "blank asset type")
	RegisterAssetExtractor("  ", func(ExtractContext) (AttributeExtraction, error) {
		return AttributeExtraction{}, nil
	})
}

func TestRegisterAssetExtractorRejectsNilExtractor(t *testing.T) {
	defer expectPanic(t, "nil extractor")
	RegisterAssetExtractor(testExtractorAssetType+"/nil", nil)
}

func TestRegisterAssetExtractorRejectsDuplicate(t *testing.T) {
	assetType := testExtractorAssetType + "/dup"
	noop := func(ExtractContext) (AttributeExtraction, error) { return AttributeExtraction{}, nil }
	RegisterAssetExtractor(assetType, noop)
	t.Cleanup(func() { unregisterAssetExtractorForTest(assetType) })
	defer expectPanic(t, "duplicate registration")
	RegisterAssetExtractor(assetType, noop)
}

func TestExtractAssetAttributesUnknownTypeIsNotHandled(t *testing.T) {
	_, handled, err := extractAssetAttributes(ExtractContext{
		AssetType: "unregistered.googleapis.com/Thing",
		Data:      json.RawMessage(`{"foo":"bar"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error for unregistered type: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false for an unregistered asset type")
	}
}

func TestExtractAssetAttributesDispatchesByAssetType(t *testing.T) {
	assetType := testExtractorAssetType + "/dispatch"
	want := AttributeExtraction{Attributes: map[string]any{"seen": true}}
	RegisterAssetExtractor(assetType, func(ctx ExtractContext) (AttributeExtraction, error) {
		if ctx.AssetType != assetType {
			t.Errorf("extractor saw asset type %q, want %q", ctx.AssetType, assetType)
		}
		return want, nil
	})
	t.Cleanup(func() { unregisterAssetExtractorForTest(assetType) })

	got, handled, err := extractAssetAttributes(ExtractContext{
		AssetType: assetType,
		Data:      json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled=true for a registered asset type")
	}
	if v, _ := got.Attributes["seen"].(bool); !v {
		t.Fatalf("expected dispatched extractor result, got %+v", got)
	}
}

func TestExtractAssetAttributesPropagatesExtractorError(t *testing.T) {
	assetType := testExtractorAssetType + "/err"
	sentinel := errors.New("boom")
	RegisterAssetExtractor(assetType, func(ExtractContext) (AttributeExtraction, error) {
		return AttributeExtraction{}, sentinel
	})
	t.Cleanup(func() { unregisterAssetExtractorForTest(assetType) })

	_, handled, err := extractAssetAttributes(ExtractContext{AssetType: assetType, Data: json.RawMessage(`{}`)})
	if !handled {
		t.Fatalf("expected handled=true even on extractor error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel error, got %v", err)
	}
}

func TestHasAssetExtractor(t *testing.T) {
	if !HasAssetExtractor(assetTypeBigQueryTable) {
		t.Errorf("expected a registered extractor for %q", assetTypeBigQueryTable)
	}
	if HasAssetExtractor("unregistered.googleapis.com/Nope") {
		t.Errorf("did not expect an extractor for an unregistered asset type")
	}
}

func expectPanic(t *testing.T, what string) {
	t.Helper()
	if r := recover(); r == nil {
		t.Fatalf("expected panic for %s, got none", what)
	}
}
