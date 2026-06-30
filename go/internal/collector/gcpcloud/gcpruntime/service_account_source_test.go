// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSourceEmitsServiceAccountTypedDepthFromFixture proves the collector runtime
// drains the offline IAM ServiceAccount CAI fixture through the credential-free
// fixture provider and emits a gcp_cloud_resource fact carrying the bounded
// typed-depth attributes and fingerprinted-email correlation anchor — the
// replayable-in-CI path with no live GCP call and no raw-email leakage.
func TestSourceEmitsServiceAccountTypedDepthFromFixture(t *testing.T) {
	scopeCfg := testScope().withDefaults()
	provider := NewFixturePageProvider(map[string][]gcpcloud.AssetsListPage{
		scopeCfg.ScopeID: {readFixturePage(t, "assets_list_service_account.json")},
	})
	src := newSource(t, testConfig(testScope()), provider, nil)

	collected, ok, err := src.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !ok {
		t.Fatal("Next returned ok=false, want a generation")
	}

	envs := drainFacts(t, collected)
	if got := countKind(envs, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	// The service account derives no outbound typed edges from its own data.
	if got := countKind(envs, facts.GCPCloudRelationshipFactKind); got != 0 {
		t.Fatalf("relationship fact count = %d, want 0", got)
	}

	resource := firstEnvelopeKind(t, envs, facts.GCPCloudResourceFactKind)
	// The typed-depth output must fingerprint the email; the full resource name
	// legitimately embeds it as the provider's canonical identity, so the leak
	// check is scoped to the extractor's own output.
	blob, err := json.Marshal(map[string]any{
		"attributes":          resource.Payload["attributes"],
		"correlation_anchors": resource.Payload["correlation_anchors"],
	})
	if err != nil {
		t.Fatalf("marshal typed-depth output: %v", err)
	}
	if strings.Contains(string(blob), "@demo-project.iam.gserviceaccount.com") {
		t.Fatalf("typed-depth output leaked a raw service-account email: %s", blob)
	}

	attrs, ok := resource.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("resource fact carried no attributes map: %#v", resource.Payload["attributes"])
	}
	if attrs["unique_id"] != "104567890123456789012" {
		t.Errorf("attributes.unique_id = %v, want 104567890123456789012", attrs["unique_id"])
	}
	fp, ok := attrs["email_fingerprint"].(string)
	if !ok || !strings.HasPrefix(fp, "sha256:") {
		t.Errorf("attributes.email_fingerprint = %v, want sha256: digest", attrs["email_fingerprint"])
	}
	anchors, ok := resource.Payload["correlation_anchors"].([]string)
	if !ok || len(anchors) != 1 || anchors[0] != fp {
		t.Fatalf("correlation_anchors = %#v, want [%s]", resource.Payload["correlation_anchors"], fp)
	}
}
