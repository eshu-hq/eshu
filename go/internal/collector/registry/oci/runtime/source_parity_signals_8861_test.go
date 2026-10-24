// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Failure-signal parity for #8861 (AC3, in-tree side): every in-band
// outcome the external producer must reproduce exactly — bounded-window
// truncation, retryable transport/status skips, partial referrer
// visibility, and stale-claim rejection. Shares the live oracle in
// source_parity_8861_test.go.

package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/distribution"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestOCI8861ParityTruncationIsInBand pins the bounded-window signal: an
// over-limit tag list still commits what fits plus an
// oci_registry.warning with code tag_list_truncated. The external producer
// must reproduce the warning, not silently drop the tail.
func TestOCI8861ParityTruncationIsInBand(t *testing.T) {
	t.Parallel()

	live := parityLiveSource(t)
	var collected collector.CollectedGeneration
	for {
		next, ok, err := live.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			t.Fatal("Next() exhausted before the truncated scope")
		}
		if next.Scope.ScopeID == "oci-registry://ghcr.io/eshu-hq/parity-demo" {
			collected = next
			break
		}
	}
	envelopes := drainEnvelopes(collected)
	var warnings []facts.Envelope
	var tags int
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case chain.OCIRegistryWarningFactKind:
			warnings = append(warnings, envelope)
		case chain.OCIImageTagObservationFactKind:
			tags++
		}
	}
	if tags != 1 {
		t.Fatalf("tag observations = %d, want 1 bounded result", tags)
	}
	truncated := false
	for _, warning := range warnings {
		if code, _ := warning.Payload["warning_code"].(string); code == oci.WarningTagListTruncated {
			truncated = true
		}
	}
	if !truncated {
		t.Fatalf("no %q warning among %d warnings", oci.WarningTagListTruncated, len(warnings))
	}
}

// TestOCI8861ParityRetryableTransportSkipsTarget pins the retryable split:
// a transient transport failure skips the target without exiting the
// collector, and a 503 does the same. Both map to SDK Result retryable at
// extraction (SHIM_MAP.md).
func TestOCI8861ParityRetryableTransportSkipsTarget(t *testing.T) {
	t.Parallel()

	newSource := func(first RegistryClient) *Source {
		healthy := &stubRegistryClient{tags: []string{"latest"}}
		return &Source{
			Config: Config{
				CollectorInstanceID: "oci-parity-8861",
				Targets: []TargetConfig{
					{Provider: oci.ProviderGHCR, Registry: "ghcr.io", Repository: "team/first", TagLimit: 1},
					{Provider: oci.ProviderGHCR, Registry: "ghcr.io", Repository: "team/second", TagLimit: 1},
				},
			},
			ClientFactory: ClientFactoryFunc(func(_ context.Context, target TargetConfig) (RegistryClient, error) {
				if target.Repository == "team/first" {
					return first, nil
				}
				return healthy, nil
			}),
		}
	}

	transport := newSource(&pingFailingClient{pingErr: connectionReset()})
	collected, ok, err := transport.Next(context.Background())
	if err != nil {
		t.Fatalf("transport Next() error = %v, want skip not exit", err)
	}
	if !ok || collected.Scope.ScopeID == "" {
		t.Fatal("transport Next() must scan the healthy target after skipping")
	}

	retryable := newSource(&pingFailingClient{pingErr: retryableStatus(t, 503)})
	collected, ok, err = retryable.Next(context.Background())
	if err != nil {
		t.Fatalf("status Next() error = %v, want skip not exit", err)
	}
	if !ok || collected.Scope.ScopeID == "" {
		t.Fatal("status Next() must scan the healthy target after skipping")
	}
}

// TestOCI8861ParityPartialReferrersStayComplete pins partial visibility: a
// failed Referrers API call degrades to an unsupported_referrers_api warning
// while the tag and manifest facts still commit.
func TestOCI8861ParityPartialReferrersStayComplete(t *testing.T) {
	t.Parallel()

	client := &parityReferrerFailClient{
		stubRegistryClient: stubRegistryClient{
			tags: []string{"latest"},
			manifest: distribution.ManifestResponse{
				Digest:    testManifestDigest,
				MediaType: oci.MediaTypeOCIImageManifest,
				Body:      testManifestBody(t),
				SizeBytes: 512,
			},
		},
		referrersErr: context.DeadlineExceeded,
	}
	source := &Source{
		Config: Config{
			CollectorInstanceID: "oci-parity-8861",
			Targets: []TargetConfig{{
				Provider:     oci.ProviderDockerHub,
				Registry:     "registry-1.docker.io",
				Repository:   "library/busybox",
				TagLimit:     10,
				SourceURI:    "https://registry-1.docker.io/v2/library/busybox",
				FencingToken: 7,
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
		Clock: func() time.Time { return parityObservedAt },
	}
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want the degraded scope")
	}
	envelopes := drainEnvelopes(collected)
	counts := kindMultiset(envelopes)
	if counts[chain.OCIImageTagObservationFactKind] != 1 || counts[chain.OCIImageManifestFactKind] != 1 {
		t.Fatalf("reachable evidence missing: kinds = %v", counts)
	}
	if counts[chain.OCIImageReferrerFactKind] != 0 {
		t.Fatalf("referrer facts = %d, want 0 on Referrers failure", counts[chain.OCIImageReferrerFactKind])
	}
	unsupported := false
	for _, envelope := range envelopes {
		if envelope.FactKind != chain.OCIRegistryWarningFactKind {
			continue
		}
		if code, _ := envelope.Payload["warning_code"].(string); code == oci.WarningUnsupportedReferrersAPI {
			unsupported = true
		}
	}
	if !unsupported {
		t.Fatalf("no %q warning; kinds = %v", oci.WarningUnsupportedReferrersAPI, counts)
	}
}

// TestOCI8861ParityStaleClaimRejected pins the claim-routing half of stale
// handling: a work item for another instance, or duplicate configured targets
// for one scope, is rejected before any scan. Generation reuse (same claim
// generation echoed back) keeps retries idempotent.
func TestOCI8861ParityStaleClaimRejected(t *testing.T) {
	t.Parallel()

	observedAt := parityObservedAt
	client := &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			Digest:    testManifestDigest,
			MediaType: oci.MediaTypeOCIImageManifest,
			Body:      testManifestBody(t),
			SizeBytes: 512,
		},
	}
	newClaimed := func(targets []TargetConfig) ClaimedSource {
		return ClaimedSource{Source: Source{
			Config:        Config{CollectorInstanceID: "oci-parity-8861", Targets: targets},
			ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) { return client, nil }),
			Clock:         func() time.Time { return observedAt },
		}}
	}
	target := TargetConfig{
		Provider: oci.ProviderDockerHub, Registry: "registry-1.docker.io",
		Repository: "library/busybox", TagLimit: 1, FencingToken: 3,
	}
	item := workflow.WorkItem{
		WorkItemID: "oci-item-1", RunID: "oci-run-1",
		CollectorKind:       scope.CollectorOCIRegistry,
		CollectorInstanceID: "oci-parity-8861",
		SourceSystem:        string(scope.CollectorOCIRegistry),
		ScopeID:             "oci-registry://registry-1.docker.io/library/busybox",
		AcceptanceUnitID:    "oci-registry://registry-1.docker.io/library/busybox",
		SourceRunID:         "oci-generation-1",
		GenerationID:        "oci-generation-1",
		CurrentFencingToken: 9,
		Status:              workflow.WorkItemStatusClaimed,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	stale := item
	stale.CollectorInstanceID = "oci-parity-other"
	if _, _, err := newClaimed([]TargetConfig{target}).NextClaimed(context.Background(), stale); err == nil {
		t.Fatal("NextClaimed() error = nil for a foreign instance claim, want rejection")
	}

	if _, _, err := newClaimed([]TargetConfig{target, target}).NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed() error = nil for duplicate matching targets, want rejection")
	}

	first, ok, err := newClaimed([]TargetConfig{target}).NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = (%v, %v), want (true, nil)", ok, err)
	}
	second, ok, err := newClaimed([]TargetConfig{target}).NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("retry NextClaimed() = (%v, %v), want (true, nil)", ok, err)
	}
	if first.Generation.GenerationID != item.GenerationID || second.Generation.GenerationID != item.GenerationID {
		t.Fatalf("claim generation not reused: %q vs %q",
			first.Generation.GenerationID, second.Generation.GenerationID)
	}
	for _, envelope := range drainEnvelopes(first) {
		if envelope.FencingToken != item.CurrentFencingToken {
			t.Fatalf("FencingToken = %d, want claim token %d", envelope.FencingToken, item.CurrentFencingToken)
		}
	}
}
