// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"
	"reflect"
	"testing"

	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// gcpRelationshipEnvelopeForSkipTest builds a schema-major-1
// gcp_cloud_relationship envelope whose payload carries the flat named
// identity keys PLUS a control-plane Attributes map padded with padKeys extra
// keys, mirroring the emitter-shaped corpus
// go/internal/relationships/gcp_evidence_bench_test.go uses. It is the shared
// fixture for the WithoutAttributesRemainder equivalence test and benchmark.
func gcpRelationshipEnvelopeForSkipTest(t testing.TB, padKeys int) Envelope {
	t.Helper()
	sourceAsset := "run.googleapis.com/Service"
	targetAsset := "secretmanager.googleapis.com/Secret"
	support := "supported"
	attrs := map[string]any{
		"collector_instance_id":    "collector-1",
		"parent_scope_kind":        "project",
		"parent_scope_id":          "demo",
		"asset_type_family":        "compute",
		"content_family":           "runtime",
		"location_bucket":          "us-central1",
		"source_project_id":        "demo",
		"target_project_id":        "demo",
		"redaction_policy_version": "1.0.0",
		"read_time":                "2026-07-07T00:00:00Z",
		"update_time":              "2026-07-07T00:00:00Z",
	}
	for k := 0; k < padKeys; k++ {
		attrs[fmt.Sprintf("pad_key_%02d", k)] = fmt.Sprintf("pad-value-%d", k)
	}
	payload, err := EncodeGCPCloudRelationship(gcpv1.Relationship{
		SourceFullResourceName: "//run.googleapis.com/projects/demo/locations/us-central1/services/service-0001",
		TargetFullResourceName: "//secretmanager.googleapis.com/projects/demo/secrets/secret-0001",
		RelationshipType:       "run_service_uses_secret",
		SourceAssetType:        &sourceAsset,
		TargetAssetType:        &targetAsset,
		SupportState:           &support,
		Attributes:             attrs,
	})
	if err != nil {
		t.Fatalf("EncodeGCPCloudRelationship: %v", err)
	}
	return Envelope{
		FactKind:      FactKindGCPCloudRelationship,
		SchemaVersion: "1.0.0",
		Payload:       payload,
	}
}

// TestDecodeGCPCloudRelationship_WithoutAttributesRemainder is the
// exact-equivalence proof for issue #4865: the WithoutAttributesRemainder
// opt-in decodes every named struct field identically to the default decode,
// and differs only in leaving Attributes empty. A named-field-only caller
// (go/internal/relationships/gcp_evidence.go) reads only the named fields, so
// the empty Attributes is a discarded-work saving, never a truth change.
func TestDecodeGCPCloudRelationship_WithoutAttributesRemainder(t *testing.T) {
	for _, padKeys := range []int{0, 24} {
		env := gcpRelationshipEnvelopeForSkipTest(t, padKeys)

		full, err := DecodeGCPCloudRelationship(env)
		if err != nil {
			t.Fatalf("default DecodeGCPCloudRelationship: %v", err)
		}
		named, err := DecodeGCPCloudRelationship(env, WithoutAttributesRemainder())
		if err != nil {
			t.Fatalf("WithoutAttributesRemainder DecodeGCPCloudRelationship: %v", err)
		}

		// Every named field must be identical between the two decodes.
		if full.SourceFullResourceName != named.SourceFullResourceName ||
			full.TargetFullResourceName != named.TargetFullResourceName ||
			full.RelationshipType != named.RelationshipType {
			t.Fatalf("padKeys=%d: named string fields diverged: full=%+v named=%+v", padKeys, full, named)
		}
		if !reflect.DeepEqual(full.SourceAssetType, named.SourceAssetType) ||
			!reflect.DeepEqual(full.TargetAssetType, named.TargetAssetType) ||
			!reflect.DeepEqual(full.SupportState, named.SupportState) {
			t.Fatalf("padKeys=%d: named pointer fields diverged: full=%+v named=%+v", padKeys, full, named)
		}

		// The default decode populates Attributes; the opt-in leaves it empty.
		if len(full.Attributes) == 0 {
			t.Fatalf("padKeys=%d: expected default decode to populate Attributes, got empty", padKeys)
		}
		if len(named.Attributes) != 0 {
			t.Fatalf("padKeys=%d: expected WithoutAttributesRemainder to leave Attributes empty, got %d keys",
				padKeys, len(named.Attributes))
		}
	}
}

// TestDecodeGCPCloudRelationship_DefaultUnchanged guards the additive contract:
// a decode with no options still populates the full Attributes remainder, so
// existing callers that read .Attributes (for example the reducer's own decode
// site, go/internal/reducer/factschema_decode.go) keep their prior behavior.
func TestDecodeGCPCloudRelationship_DefaultUnchanged(t *testing.T) {
	env := gcpRelationshipEnvelopeForSkipTest(t, 3)
	full, err := DecodeGCPCloudRelationship(env)
	if err != nil {
		t.Fatalf("DecodeGCPCloudRelationship: %v", err)
	}
	// The 11 control-plane keys plus 3 pad keys are all non-named, so all 14
	// land in Attributes.
	if got := len(full.Attributes); got != 14 {
		t.Fatalf("expected 14 Attributes keys on default decode, got %d: %v", got, full.Attributes)
	}
	if _, ok := full.Attributes["collector_instance_id"]; !ok {
		t.Fatalf("expected control-plane key in Attributes, got %v", full.Attributes)
	}
}

// BenchmarkDecodeGCPCloudRelationship measures the shipped decode mechanism
// (not the Phase-1 prototype): the default full decode versus the
// WithoutAttributesRemainder opt-in, on the realistic (~11-key) and worst-case
// wide (~35-key) Attributes shapes. It is the No-Regression Evidence for issue
// #4865, run through the real public DecodeGCPCloudRelationship seam the
// reducer and relationships callers use.
func BenchmarkDecodeGCPCloudRelationship(b *testing.B) {
	b.Run("Full/RealisticAttributes_11key", func(b *testing.B) {
		benchmarkDecodeGCPCloudRelationship(b, 0, false)
	})
	b.Run("Full/WorstCaseWideAttributes_35key", func(b *testing.B) {
		benchmarkDecodeGCPCloudRelationship(b, 24, false)
	})
	b.Run("WithoutAttributesRemainder/RealisticAttributes_11key", func(b *testing.B) {
		benchmarkDecodeGCPCloudRelationship(b, 0, true)
	})
	b.Run("WithoutAttributesRemainder/WorstCaseWideAttributes_35key", func(b *testing.B) {
		benchmarkDecodeGCPCloudRelationship(b, 24, true)
	})
}

func benchmarkDecodeGCPCloudRelationship(b *testing.B, padKeys int, skipRemainder bool) {
	env := gcpRelationshipEnvelopeForSkipTest(b, padKeys)
	var opts []DecodeOption
	if skipRemainder {
		opts = []DecodeOption{WithoutAttributesRemainder()}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := DecodeGCPCloudRelationship(env, opts...); err != nil {
			b.Fatalf("DecodeGCPCloudRelationship: %v", err)
		}
	}
}
