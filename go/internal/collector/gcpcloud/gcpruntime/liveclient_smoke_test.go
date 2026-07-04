// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/impersonate"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// boundedPageProvider wraps a PageProvider and hard-stops page draining after
// maxPages pages for a scope, regardless of what the wrapped provider still
// has to offer. It reports the wrapped page's NextPageToken as empty once the
// page cap is hit so Source.Next stops fetching instead of draining the whole
// scope. This keeps the live smoke cost-bounded at the provider seam rather
// than only bounding how many facts the test counts afterward.
type boundedPageProvider struct {
	inner    PageProvider
	maxPages int

	pagesServed int
}

// FetchPage delegates to the wrapped provider, then clears NextPageToken once
// maxPages pages have been served so the caller (Source) treats the scope as
// drained.
func (p *boundedPageProvider) FetchPage(ctx context.Context, req PageRequest) (gcpcloud.AssetsListPage, error) {
	page, err := p.inner.FetchPage(ctx, req)
	if err != nil {
		return page, err
	}
	p.pagesServed++
	if p.pagesServed >= p.maxPages {
		page.NextPageToken = ""
	}
	return page, nil
}

var _ PageProvider = (*boundedPageProvider)(nil)

// TestLiveSmokeCloudAssetInventory is a gated, read-only live smoke test
// against a real Cloud Asset Inventory organization scope. It never runs in
// CI: it is skipped unless ESHU_GCP_LIVE_SMOKE=1 is set in the environment,
// and it never wires LiveClient as a default provider (see AGENTS.md). It
// authenticates via keyless service-account impersonation (no key file, no
// hardcoded identifiers) and only issues GET assets.list requests through
// LiveClient.FetchPage; see liveclient.go for the read-only request path.
//
// Target identity is supplied entirely through environment variables at
// runtime so no organization id, service-account email, or project id is
// ever committed to this file. Once ESHU_GCP_LIVE_SMOKE=1 is set, the target
// environment variables are required; a missing one fails the test instead of
// skipping it, so a live-smoke run cannot go green without exercising the live
// path. See AGENTS.md for the scoped exception this test carries against the
// package's hermetic-httptest-only rule.
func TestLiveSmokeCloudAssetInventory(t *testing.T) {
	if os.Getenv("ESHU_GCP_LIVE_SMOKE") != "1" {
		t.Skip("live smoke gated: set ESHU_GCP_LIVE_SMOKE=1 to run")
	}

	sa := strings.TrimSpace(os.Getenv("ESHU_GCP_SMOKE_SA"))
	org := strings.TrimSpace(os.Getenv("ESHU_GCP_SMOKE_ORG"))
	quotaProject := strings.TrimSpace(os.Getenv("ESHU_GCP_SMOKE_QUOTA_PROJECT"))
	if sa == "" || org == "" || quotaProject == "" {
		t.Fatal("ESHU_GCP_LIVE_SMOKE=1 requires ESHU_GCP_SMOKE_SA, ESHU_GCP_SMOKE_ORG, and ESHU_GCP_SMOKE_QUOTA_PROJECT to all be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: sa,
		Scopes:          []string{CloudAssetInventoryOAuthScope},
	})
	if err != nil {
		t.Fatalf("impersonate.CredentialsTokenSource: %v", err)
	}

	lc := LiveClient{
		TokenSource:    ts,
		QuotaProjectID: quotaProject,
	}

	// Exercise the read-only redaction-key-file loader path: generate fresh
	// random key material, write it to a 0600 temp file, and load it back
	// through the same os.ReadFile + redact.NewKey path the collector command
	// uses (see loadRedactionKey in cmd/collector-gcp-cloud/main.go). This
	// keeps any redaction markers produced by this run non-reproducible
	// outside the run and proves the file-mount gate item, not just the
	// in-memory key constructor.
	keyMaterial := make([]byte, 32)
	if _, err := rand.Read(keyMaterial); err != nil {
		t.Fatalf("crypto/rand.Read: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "redaction.key")
	if err := os.WriteFile(keyPath, keyMaterial, 0o600); err != nil {
		t.Fatalf("write redaction key file: %v", err)
	}
	keyFileBytes, err := os.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		t.Fatalf("read redaction key file: %v", err)
	}
	key, err := redact.NewKey(keyFileBytes)
	if err != nil {
		t.Fatalf("redact.NewKey: %v", err)
	}

	scopeCfg := ScopeConfig{
		ParentScopeKind: gcpcloud.ParentScopeOrganization,
		ParentScopeID:   org,
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		GenerationID:    "smoke-gen-1",
		FencingToken:    1,
		CredentialRef:   "smoke-credential-ref",
	}
	cfg := Config{
		CollectorInstanceID: "gcp-live-smoke",
		Scopes:              []ScopeConfig{scopeCfg},
	}

	// Bound the live scan at the provider seam: at most maxPages assets.list
	// pages are served for the scope regardless of how many the org actually
	// has, so the smoke stays cost-bounded even against a large org.
	const maxPages = 5
	provider := &boundedPageProvider{inner: lc, maxPages: maxPages}

	src := &Source{
		Config:       cfg,
		Provider:     provider,
		RedactionKey: key,
	}

	const maxFacts = 5000

	var (
		totalFacts       int
		factKindCounts   = map[string]int{}
		assetTypeCounts  = map[string]int{}
		warningCount     int
		missingRedaction int
	)

	for {
		gen, more, nextErr := src.Next(ctx)
		if nextErr != nil {
			t.Fatalf("Source.Next: %v", nextErr)
		}
		if !more {
			// Source.Next returns a zero-value CollectedGeneration (with a nil
			// Facts channel) once the configured scope batch is drained.
			// Ranging over a nil channel blocks forever, so stop before that
			// range rather than after it.
			break
		}
		for envelope := range gen.Facts {
			totalFacts++
			factKindCounts[envelope.FactKind]++
			switch envelope.FactKind {
			case facts.GCPCloudResourceFactKind:
				assetType, _ := envelope.Payload["asset_type"].(string)
				if assetType != "" {
					assetTypeCounts[assetType]++
				}
				policyVersion, _ := envelope.Payload["redaction_policy_version"].(string)
				if strings.TrimSpace(policyVersion) == "" {
					missingRedaction++
				}
			case facts.GCPCollectionWarningFactKind:
				warningCount++
			}
			if totalFacts >= maxFacts {
				break
			}
		}
		if gen.FactStreamErr != nil {
			if streamErr := gen.FactStreamErr(); streamErr != nil {
				t.Fatalf("gen.FactStreamErr: %v", streamErr)
			}
		}
		if !more || totalFacts >= maxFacts {
			break
		}
	}

	if totalFacts == 0 {
		t.Fatal("live smoke collected zero facts; want real gcp_cloud_resource data to flow")
	}
	if factKindCounts[facts.GCPCloudResourceFactKind] == 0 {
		t.Fatalf("live smoke collected zero %s facts", facts.GCPCloudResourceFactKind)
	}
	if missingRedaction > 0 {
		t.Fatalf("%d gcp_cloud_resource facts are missing redaction_policy_version", missingRedaction)
	}

	assetTypeKinds := make([]string, 0, len(assetTypeCounts))
	for k := range assetTypeCounts {
		assetTypeKinds = append(assetTypeKinds, k)
	}
	sort.Strings(assetTypeKinds)

	t.Logf("live smoke: pages_served=%d (bounded at %d)", provider.pagesServed, maxPages)
	t.Logf("live smoke: total_facts=%d", totalFacts)
	t.Logf("live smoke: fact_kind_histogram=%v", factKindCounts)
	t.Logf("live smoke: asset_type_kinds=%v", assetTypeKinds)
	t.Logf("live smoke: asset_type_histogram=%v", assetTypeCounts)
	t.Logf("live smoke: warning_count=%d", warningCount)
	t.Logf("live smoke: redaction_policy_version_present=%t", missingRedaction == 0)
}
