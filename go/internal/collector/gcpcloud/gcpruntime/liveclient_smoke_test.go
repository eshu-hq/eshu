// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/impersonate"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

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
// ever committed to this file.
func TestLiveSmokeCloudAssetInventory(t *testing.T) {
	if os.Getenv("ESHU_GCP_LIVE_SMOKE") != "1" {
		t.Skip("live smoke gated: set ESHU_GCP_LIVE_SMOKE=1 to run")
	}

	sa := strings.TrimSpace(os.Getenv("ESHU_GCP_SMOKE_SA"))
	org := strings.TrimSpace(os.Getenv("ESHU_GCP_SMOKE_ORG"))
	quotaProject := strings.TrimSpace(os.Getenv("ESHU_GCP_SMOKE_QUOTA_PROJECT"))
	if sa == "" || org == "" || quotaProject == "" {
		t.Skip("live smoke gated: ESHU_GCP_SMOKE_SA, ESHU_GCP_SMOKE_ORG, ESHU_GCP_SMOKE_QUOTA_PROJECT must all be set")
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

	key, err := redact.NewKey([]byte("gcp-live-smoke-ephemeral-key-material"))
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

	src := &Source{
		Config:       cfg,
		Provider:     lc,
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

	t.Logf("live smoke: total_facts=%d", totalFacts)
	t.Logf("live smoke: fact_kind_histogram=%v", factKindCounts)
	t.Logf("live smoke: asset_type_kinds=%v", assetTypeKinds)
	t.Logf("live smoke: asset_type_histogram=%v", assetTypeCounts)
	t.Logf("live smoke: warning_count=%d", warningCount)
	t.Logf("live smoke: redaction_policy_version_present=%t", missingRedaction == 0)
}
