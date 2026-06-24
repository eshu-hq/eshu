// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func resourceChangesBoundary() Boundary {
	boundary := testBoundary()
	boundary.SourceLane = SourceLaneResourceChanges
	boundary.ScopeID = "azure:tenant-abc:subscription:11111111:microsoft.compute:eastus:resource_changes"
	return boundary
}

func TestParseResourceChangesPage(t *testing.T) {
	page, err := ParseResourceChangesPage(loadFixture(t, "resourcechanges_page1.json"))
	if err != nil {
		t.Fatalf("ParseResourceChangesPage error: %v", err)
	}
	if page.Count != 2 {
		t.Fatalf("Count = %d, want 2", page.Count)
	}
	if len(page.Changes) != 2 {
		t.Fatalf("len(Changes) = %d, want 2", len(page.Changes))
	}
	first := page.Changes[0]
	if got := first.TargetARMResourceID(); !strings.Contains(got, "virtualMachines/vm-web-01") {
		t.Fatalf("target arm id = %q", got)
	}
	if got := first.ChangeType(); got != ChangeTypeUpdated {
		t.Fatalf("change type = %q, want %q", got, ChangeTypeUpdated)
	}
	if got := first.ChangedPropertyPaths(); len(got) != 2 {
		t.Fatalf("changed paths = %v, want 2 paths", got)
	}
	if first.ChangeTime() == nil {
		t.Fatal("change time was not parsed")
	}
}

func TestCollectResourceChangesEmitsBoundedFacts(t *testing.T) {
	page, err := ParseResourceChangesPage(loadFixture(t, "resourcechanges_page1.json"))
	if err != nil {
		t.Fatalf("parse changes: %v", err)
	}
	provider := &fixturePageProvider{changePages: map[string]ResourceChangesPage{"": page}}
	result, err := NewCollector(provider, nil, WithRedactionKey(testRedactionKey(t))).Collect(
		context.Background(),
		resourceChangesBoundary(),
	)
	if err != nil {
		t.Fatalf("collect changes: %v", err)
	}
	changes := factsOfKind(result.Facts, facts.AzureResourceChangeFactKind)
	if len(changes) != 2 {
		t.Fatalf("emitted %d change facts, want 2", len(changes))
	}
	if len(factsOfKind(result.Facts, facts.AzureCloudResourceFactKind)) != 0 {
		t.Fatal("resource-change lane must not emit azure_cloud_resource facts")
	}
	if result.ResourceChangeCount != 2 {
		t.Fatalf("ResourceChangeCount = %d, want 2", result.ResourceChangeCount)
	}
	first := changes[0]
	if first.Payload["source_lane"] != SourceLaneResourceChanges {
		t.Fatalf("source_lane = %v", first.Payload["source_lane"])
	}
	if first.Payload["actor_fingerprint"] == "actor-raw-guid-0001" {
		t.Fatal("raw changedBy actor leaked into payload")
	}
	if first.Payload["is_tombstone_candidate"] != false {
		t.Fatalf("first tombstone candidate = %v, want false", first.Payload["is_tombstone_candidate"])
	}
	if changes[1].Payload["is_tombstone_candidate"] != true {
		t.Fatalf("delete tombstone candidate = %v, want true", changes[1].Payload["is_tombstone_candidate"])
	}
	assertNoRawChangePayload(t, changes)
}

func TestCollectResourceChangesRequiresRedactionKey(t *testing.T) {
	page, err := ParseResourceChangesPage(loadFixture(t, "resourcechanges_page1.json"))
	if err != nil {
		t.Fatalf("parse changes: %v", err)
	}
	provider := &fixturePageProvider{changePages: map[string]ResourceChangesPage{"": page}}
	if _, err := NewCollector(provider, nil).Collect(context.Background(), resourceChangesBoundary()); err == nil {
		t.Fatal("expected resource-change collection to fail closed without a redaction key")
	}
}

func TestCollectResourceChangesEmptyState(t *testing.T) {
	page, err := ParseResourceChangesPage(loadFixture(t, "resourcechanges_empty.json"))
	if err != nil {
		t.Fatalf("parse changes: %v", err)
	}
	provider := &fixturePageProvider{changePages: map[string]ResourceChangesPage{"": page}}
	result, err := NewCollector(provider, nil, WithRedactionKey(testRedactionKey(t))).Collect(
		context.Background(),
		resourceChangesBoundary(),
	)
	if err != nil {
		t.Fatalf("collect empty changes: %v", err)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("facts = %d, want empty state", len(result.Facts))
	}
	if result.ResourceChangeCount != 0 {
		t.Fatalf("ResourceChangeCount = %d, want 0", result.ResourceChangeCount)
	}
}

func assertNoRawChangePayload(t *testing.T, envs []facts.Envelope) {
	t.Helper()
	raw, err := json.Marshal(envs)
	if err != nil {
		t.Fatalf("marshal payloads: %v", err)
	}
	for _, forbidden := range []string{
		"actor-raw-guid-0001",
		"actor-raw-guid-0002",
		"beforeValue",
		"afterValue",
		"Standard_D2s_v3",
		"Standard_D4s_v3",
		"raw-body-must-not-persist",
		"responseBody",
		"credential_ref",
		"azure-read-only-spn",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("resource change payload leaked %q: %s", forbidden, raw)
		}
	}
}
