// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestLogSinkOfflineFixtureEndToEnd exercises the offline assets.list fixture for
// Logging Log Sink through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes and
// the destination export edges reach durable facts without any live GCP call, and
// that neither the writer-identity email nor any filter text lands on a fact.
func TestLogSinkOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_log_sink.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(page.Resources))
	}

	gen := NewGeneration(attributesTestBoundary(), redact.Key{})
	if err := gen.AddPage(page.Resources); err != nil {
		t.Fatalf("add page: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("build generation: %v", err)
	}

	resourceCount := 0
	datasetEdges := 0
	bucketEdges := 0
	var auditAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == logSinkFullName {
				auditAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeLogSinkExportsToDataset:
				datasetEdges++
			case relationshipTypeLogSinkExportsToBucket:
				bucketEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if auditAttrs == nil || auditAttrs["destination_type"] != "bigquery" {
		t.Errorf("audit sink attrs missing/incorrect: %#v", auditAttrs)
	}
	if auditAttrs["writer_identity_email_fingerprint"] == nil {
		t.Errorf("audit sink missing writer identity fingerprint: %#v", auditAttrs)
	}
	if datasetEdges != 1 {
		t.Errorf("log_sink_exports_to_dataset edges = %d, want 1", datasetEdges)
	}
	if bucketEdges != 1 {
		t.Errorf("log_sink_exports_to_bucket edges = %d, want 1", bucketEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"gcp-sa-logging.iam.gserviceaccount.com", "writerIdentity", "cloudaudit.googleapis.com"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked sensitive token %q", token)
		}
	}
}
