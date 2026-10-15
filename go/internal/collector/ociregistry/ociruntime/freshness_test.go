// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestOCIFreshnessHintSkipsIdenticalTick proves two ticks over identical
// registry state share one non-empty freshness hint, so the store can skip
// the second generation instead of minting per tick (#8003). The ticks use
// different observation clocks to prove the hint covers content, not time.
func TestOCIFreshnessHintSkipsIdenticalTick(t *testing.T) {
	t.Parallel()

	first := scanTick(t, time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC), testManifestDigest, false)
	second := scanTick(t, time.Date(2026, 5, 12, 14, 10, 0, 0, time.UTC), testManifestDigest, false)

	if strings.TrimSpace(first.Generation.FreshnessHint) == "" {
		t.Fatal("first FreshnessHint is blank, want stable content digest")
	}
	if got, want := second.Generation.FreshnessHint, first.Generation.FreshnessHint; got != want {
		t.Fatalf("FreshnessHint changed for unchanged registry state: got %q, want %q", got, want)
	}
	candidate := backfill.UnchangedGenerationCandidate{
		GenerationID:  "oci-generation-1",
		FreshnessHint: first.Generation.FreshnessHint,
		Status:        scope.GenerationStatusActive,
	}
	action, _ := backfill.DecideUnchangedGeneration(second.Generation, candidate, true)
	if action != backfill.UnchangedActionSkip {
		t.Fatalf("DecideUnchangedGeneration() = %d, want skip (%d)", action, backfill.UnchangedActionSkip)
	}
}

// TestOCIFreshnessHintStoresRotatedDigest proves a changed manifest digest
// mints a new generation instead of skipping over real drift (#8003).
func TestOCIFreshnessHintStoresRotatedDigest(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)
	first := scanTick(t, observedAt, testManifestDigest, false)
	rotated := "sha256:5555555555555555555555555555555555555555555555555555555555555555"
	second := scanTick(t, observedAt.Add(10*time.Minute), rotated, false)

	if first.Generation.FreshnessHint == second.Generation.FreshnessHint {
		t.Fatalf("FreshnessHint did not change after digest rotation: %q", first.Generation.FreshnessHint)
	}
	candidate := backfill.UnchangedGenerationCandidate{
		GenerationID:  "oci-generation-1",
		FreshnessHint: first.Generation.FreshnessHint,
		Status:        scope.GenerationStatusActive,
	}
	action, _ := backfill.DecideUnchangedGeneration(second.Generation, candidate, true)
	if action != backfill.UnchangedActionStore {
		t.Fatalf("DecideUnchangedGeneration() = %d, want store (%d)", action, backfill.UnchangedActionStore)
	}
}

// TestOCIFreshnessHintStoresReferrerDrift proves a new referrer (for example
// a freshly published attestation) mints a new generation even when every
// reference digest is unchanged (#8003 F1). A reference-digest-only hint
// would skip over referrer and attestation truth.
func TestOCIFreshnessHintStoresReferrerDrift(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)
	withoutReferrer := scanTickWithReferrers(t, observedAt, testManifestDigest, false, distribution.ReferrersResponse{})
	withReferrer := scanTickWithReferrers(t, observedAt.Add(10*time.Minute), testManifestDigest, false, distribution.ReferrersResponse{Referrers: []ociregistry.Descriptor{{
		Digest:       testReferrerDigest,
		MediaType:    ociregistry.MediaTypeOCIImageManifest,
		SizeBytes:    128,
		ArtifactType: "application/vnd.in-toto+json",
	}}})

	if withoutReferrer.Generation.FreshnessHint == withReferrer.Generation.FreshnessHint {
		t.Fatalf("FreshnessHint did not change when a referrer appeared: %q", withoutReferrer.Generation.FreshnessHint)
	}
	candidate := backfill.UnchangedGenerationCandidate{
		GenerationID:  "oci-generation-1",
		FreshnessHint: withoutReferrer.Generation.FreshnessHint,
		Status:        scope.GenerationStatusActive,
	}
	action, _ := backfill.DecideUnchangedGeneration(withReferrer.Generation, candidate, true)
	if action != backfill.UnchangedActionStore {
		t.Fatalf("DecideUnchangedGeneration() = %d, want store (%d)", action, backfill.UnchangedActionStore)
	}
}

// TestOCIFreshnessHintEncodesWarningOutcomes proves the hint folds in
// referrer-API and config-blob warning outcomes, so a warning appearing,
// clearing, or changing text cannot masquerade as an unchanged tick
// (#8003 F1). Referrer and warning input order never changes the hint.
func TestOCIFreshnessHintEncodesWarningOutcomes(t *testing.T) {
	t.Parallel()

	refs := []ociReferenceDigest{{reference: "latest", digest: testManifestDigest}}
	withoutWarning := ociFreshnessHint(refs, nil, nil, false)
	withWarning := ociFreshnessHint(refs, nil, []ociWarningOutcome{{code: ociregistry.WarningConfigBlobUnavailable, message: "image config blob unavailable for provenance label read", digest: testConfigDigest}}, false)
	if withoutWarning == withWarning {
		t.Fatal("FreshnessHint did not change when a config warning appeared")
	}
	withReferrer := ociFreshnessHint(refs, []ociReferrerIdentity{{subjectDigest: testManifestDigest, digest: testReferrerDigest, mediaType: ociregistry.MediaTypeOCIImageManifest, artifactType: "application/vnd.in-toto+json"}}, nil, false)
	if withoutWarning == withReferrer {
		t.Fatal("FreshnessHint did not change when a referrer identity appeared")
	}

	forwardReferrers := []ociReferrerIdentity{
		{subjectDigest: "s1", digest: "d1", mediaType: "m", artifactType: "a"},
		{subjectDigest: "s2", digest: "d2", mediaType: "m", artifactType: "a"},
	}
	reversedReferrers := []ociReferrerIdentity{forwardReferrers[1], forwardReferrers[0]}
	if got, want := ociFreshnessHint(refs, forwardReferrers, nil, false), ociFreshnessHint(refs, reversedReferrers, nil, false); got != want {
		t.Fatalf("ociFreshnessHint() changed when referrer order changed: %q vs %q", got, want)
	}
	forwardWarnings := []ociWarningOutcome{{code: "c1", message: "m1"}, {code: "c2", message: "m2"}}
	reversedWarnings := []ociWarningOutcome{forwardWarnings[1], forwardWarnings[0]}
	if got, want := ociFreshnessHint(refs, nil, forwardWarnings, false), ociFreshnessHint(refs, nil, reversedWarnings, false); got != want {
		t.Fatalf("ociFreshnessHint() changed when warning order changed: %q vs %q", got, want)
	}
}

// TestOCIFreshnessHintEncodesDigestGapsAndTruncation proves the hint folds in
// the digest-missing warning path and tag-list truncation, so neither state
// can masquerade as an unchanged tick (#8003).
func TestOCIFreshnessHintEncodesDigestGapsAndTruncation(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)
	withDigest := scanTick(t, observedAt, testManifestDigest, false)
	missing := scanTickMissingDigest(t, observedAt)
	if withDigest.Generation.FreshnessHint == missing.Generation.FreshnessHint {
		t.Fatal("FreshnessHint did not change when the manifest digest went missing")
	}
	if strings.TrimSpace(missing.Generation.FreshnessHint) == "" {
		t.Fatal("digest-missing FreshnessHint is blank, want encoded gap marker")
	}

	complete := scanTick(t, observedAt, testManifestDigest, false)
	truncated := scanTick(t, observedAt, testManifestDigest, true)
	if complete.Generation.FreshnessHint == truncated.Generation.FreshnessHint {
		t.Fatal("FreshnessHint did not change when the tag list was truncated")
	}

	forward := ociFreshnessHint(
		[]ociReferenceDigest{{reference: "a", digest: "d1"}, {reference: "b", digest: "d2"}},
		nil,
		nil,
		false,
	)
	reversed := ociFreshnessHint(
		[]ociReferenceDigest{{reference: "b", digest: "d2"}, {reference: "a", digest: "d1"}},
		nil,
		nil,
		false,
	)
	if forward == "" {
		t.Fatal("ociFreshnessHint() is blank, want digest")
	}
	if forward != reversed {
		t.Fatalf("ociFreshnessHint() changed when reference order changed: %q vs %q", forward, reversed)
	}
}

func scanTick(t *testing.T, observedAt time.Time, digest string, truncated bool) collectedTick {
	t.Helper()

	client := &stubRegistryClient{
		tags:              []string{"latest"},
		tagListIncomplete: truncated,
		manifest: distribution.ManifestResponse{
			Digest:    digest,
			MediaType: ociregistry.MediaTypeOCIImageManifest,
			Body:      testManifestBody(t),
			SizeBytes: 512,
		},
	}
	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-freshness-test",
			Targets: []TargetConfig{{
				Provider:   ociregistry.ProviderDockerHub,
				Registry:   "registry-1.docker.io",
				Repository: "library/busybox",
				TagLimit:   1,
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
		Clock: func() time.Time { return observedAt },
	}
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	drainFacts(t, collected)
	return collectedTick{Generation: collected.Generation}
}

func scanTickWithReferrers(t *testing.T, observedAt time.Time, digest string, truncated bool, referrers distribution.ReferrersResponse) collectedTick {
	t.Helper()

	client := &stubRegistryClient{
		tags:              []string{"latest"},
		tagListIncomplete: truncated,
		manifest: distribution.ManifestResponse{
			Digest:    digest,
			MediaType: ociregistry.MediaTypeOCIImageManifest,
			Body:      testManifestBody(t),
			SizeBytes: 512,
		},
		referrers: referrers,
	}
	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-freshness-test",
			Targets: []TargetConfig{{
				Provider:   ociregistry.ProviderDockerHub,
				Registry:   "registry-1.docker.io",
				Repository: "library/busybox",
				TagLimit:   1,
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
		Clock: func() time.Time { return observedAt },
	}
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	drainFacts(t, collected)
	return collectedTick{Generation: collected.Generation}
}

func scanTickMissingDigest(t *testing.T, observedAt time.Time) collectedTick {
	t.Helper()

	client := &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			MediaType: ociregistry.MediaTypeOCIImageManifest,
		},
	}
	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-freshness-test",
			Targets: []TargetConfig{{
				Provider:   ociregistry.ProviderDockerHub,
				Registry:   "registry-1.docker.io",
				Repository: "library/busybox",
				TagLimit:   1,
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
		Clock: func() time.Time { return observedAt },
	}
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	drainFacts(t, collected)
	return collectedTick{Generation: collected.Generation}
}

type collectedTick struct {
	Generation scope.ScopeGeneration
}
