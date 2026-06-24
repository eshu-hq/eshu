// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testResourceChangesTarget() TargetConfig {
	target := testTarget()
	target.SourceLane = azurecloud.SourceLaneResourceChanges
	return target
}

func testRuntimeRedactionKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("azure-runtime-resource-change-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return key
}

func resourceChangesFixture(t *testing.T) map[string]azurecloud.ResourceChangesPage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "resourcechanges_page1.json"))
	if err != nil {
		t.Fatalf("read changes fixture: %v", err)
	}
	page, err := azurecloud.ParseResourceChangesPage(raw)
	if err != nil {
		t.Fatalf("parse changes page: %v", err)
	}
	return map[string]azurecloud.ResourceChangesPage{"": page}
}

func TestSourceYieldsResourceChangeGenerationFromFixturePages(t *testing.T) {
	provider := NewFixtureResourceChangesPageProvider(resourceChangesFixture(t), azurecloud.ScopeAccess{})
	src := newFixtureSource(t, provider, testResourceChangesTarget())
	src.RedactionKey = testRuntimeRedactionKey(t)

	collected, ok, err := src.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next ok=%v err=%v", ok, err)
	}
	if !strings.HasSuffix(collected.Scope.ScopeID, ":"+azurecloud.SourceLaneResourceChanges) {
		t.Fatalf("scope id = %q, want resource_changes lane", collected.Scope.ScopeID)
	}
	envs := drain(t, collected)
	changes := factsOfKind(envs, facts.AzureResourceChangeFactKind)
	if len(changes) != 2 {
		t.Fatalf("emitted %d resource changes, want 2", len(changes))
	}
	if len(factsOfKind(envs, facts.AzureCloudResourceFactKind)) != 0 {
		t.Fatal("resource-change source must not emit cloud-resource facts")
	}
	assertNoRawRuntimeChangePayload(t, changes)
}

func assertNoRawRuntimeChangePayload(t *testing.T, envs []facts.Envelope) {
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
		"raw-body-must-not-persist",
		"credential_ref",
		"azure-read-only-spn",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("resource change payload leaked %q: %s", forbidden, raw)
		}
	}
}
