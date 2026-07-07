// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// BenchmarkDiscoverGCPCloudRelationshipEvidence measures the reducer-called
// gcp_cloud_relationship evidence discovery path (issue #4797, W2d) on an
// emitter-shaped corpus: every envelope carries the flat identity keys PLUS the
// merged control-plane Attributes map the collector emitter
// (gcpcloud.NewCloudRelationshipEnvelope) produces. It is the no-regression probe
// for routing the raw payloadString reads through the typed
// factschema.DecodeGCPCloudRelationship seam; the file is self-contained so it can
// be copied verbatim onto origin/main to capture the pre-conversion (raw-read)
// baseline.
//
// Two variants isolate the cost of the typed decode: "RealisticAttributes" uses
// the ~13-key Attributes map the emitter actually attaches, and
// "WorstCaseWideAttributes" pads Attributes wide to stress the remainder-map
// rebuild the typed decode performs even though the extractor reads only named
// fields.
func BenchmarkDiscoverGCPCloudRelationshipEvidence(b *testing.B) {
	b.Run("RealisticAttributes", func(b *testing.B) {
		benchmarkDiscoverGCPCloudRelationshipEvidence(b, 0)
	})
	b.Run("WorstCaseWideAttributes", func(b *testing.B) {
		benchmarkDiscoverGCPCloudRelationshipEvidence(b, 24)
	})
}

func benchmarkDiscoverGCPCloudRelationshipEvidence(b *testing.B, padKeys int) {
	const factCount = 200

	attributes := func(i int) map[string]any {
		// The control-plane Attributes the real emitter attaches (see
		// gcpcloud.NewCloudRelationshipEnvelope): identity keys are named struct
		// fields, everything here flows into the typed struct's Attributes map.
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
			attrs[fmt.Sprintf("pad_key_%02d", k)] = fmt.Sprintf("pad-value-%d-%d", i, k)
		}
		return attrs
	}

	envelopes := make([]facts.Envelope, 0, factCount)
	catalog := make([]CatalogEntry, 0, factCount*2)
	for i := 0; i < factCount; i++ {
		source := fmt.Sprintf("//run.googleapis.com/projects/demo/locations/us-central1/services/service-%04d", i)
		target := fmt.Sprintf("//secretmanager.googleapis.com/projects/demo/secrets/secret-%04d", i)
		sourceAsset := "run.googleapis.com/Service"
		targetAsset := "secretmanager.googleapis.com/Secret"
		support := "supported"
		payload, err := factschema.EncodeGCPCloudRelationship(gcpv1.Relationship{
			SourceFullResourceName: source,
			TargetFullResourceName: target,
			RelationshipType:       "run_service_uses_secret",
			SourceAssetType:        &sourceAsset,
			TargetAssetType:        &targetAsset,
			SupportState:           &support,
			Attributes:             attributes(i),
		})
		if err != nil {
			b.Fatalf("EncodeGCPCloudRelationship: %v", err)
		}
		envelopes = append(envelopes, facts.Envelope{
			FactKind:      facts.GCPCloudRelationshipFactKind,
			SchemaVersion: facts.GCPCloudRelationshipSchemaVersion,
			ScopeID:       "gcp:project:demo:relationship:global",
			StableFactKey: fmt.Sprintf("gcp-rel-%04d", i),
			Payload:       payload,
		})
		catalog = append(catalog,
			CatalogEntry{RepoID: fmt.Sprintf("repo-service-%04d", i), Aliases: []string{fmt.Sprintf("service-%04d", i)}},
			CatalogEntry{RepoID: fmt.Sprintf("repo-secret-%04d", i), Aliases: []string{fmt.Sprintf("secret-%04d", i)}},
		)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		evidence := DiscoverEvidence(envelopes, catalog)
		if len(evidence) == 0 {
			b.Fatalf("expected evidence, got none")
		}
	}
}
