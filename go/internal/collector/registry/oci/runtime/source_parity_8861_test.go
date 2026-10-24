// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtime parity pin for #8861 (AC3, in-tree side).
//
// These tests prove the CURRENT in-tree source behavior that the #4047
// cutover gate must reproduce exactly: the same scopes, kinds, stable keys,
// fencing tokens, and in-band failure signals, whether the facts come from a
// live scan or from the checked-in cassette
// (testdata/8861-parity-oci-registry.json, cut from a live run with the same
// fixed clock). The #6710 compatibility harness reuses this pack: the live
// side is the oracle, the cassette is the recorded expectation.
//
// Out of scope here (core-owned, recorded for the external producer):
// grant revocation enforcement (see PRODUCER_GRANT_DRAFT.md) and durable
// commit/lease semantics owned by the ingestion store and workflow control
// store. The in-tree path pins the emission half; the external path must
// additionally fail closed on missing, expired, or revoked grants.

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci"
	"github.com/eshu-hq/eshu/go/internal/collector/registry/oci/distribution"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

var parityObservedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// parityReferrerFailClient reports an unsupported Referrers API while serving
// every other call from the stub.
type parityReferrerFailClient struct {
	stubRegistryClient
	referrersErr error
}

func (c *parityReferrerFailClient) ListReferrers(context.Context, string, string) (distribution.ReferrersResponse, error) {
	return distribution.ReferrersResponse{}, c.referrersErr
}

// parityLiveSource rebuilds the exact source the cassette fixture was cut
// from: same targets, same stub evidence, same fixed clock, so generation
// IDs and stable keys reproduce deterministically.
func parityLiveSource(t *testing.T) *Source {
	t.Helper()
	manifest := func() distribution.ManifestResponse {
		return distribution.ManifestResponse{
			Digest:    testManifestDigest,
			MediaType: oci.MediaTypeOCIImageManifest,
			Body:      testManifestBody(t),
			SizeBytes: 512,
		}
	}
	fullClient := &stubRegistryClient{
		tags:     []string{"latest"},
		manifest: manifest(),
		referrers: distribution.ReferrersResponse{Referrers: []oci.Descriptor{{
			Digest:       testReferrerDigest,
			MediaType:    oci.MediaTypeOCIImageManifest,
			SizeBytes:    128,
			ArtifactType: "application/vnd.in-toto+json",
		}}},
	}
	partialClient := &parityReferrerFailClient{
		stubRegistryClient: stubRegistryClient{
			tags:     []string{"v2", "v1"},
			manifest: manifest(),
		},
		// Keep byte-identical with the recorded cassette: the error text
		// feeds the warning message and therefore its stable key.
		referrersErr: errors.New("referrers API not supported by this registry"),
	}
	indexBody, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     oci.MediaTypeOCIImageIndex,
		"manifests": []map[string]any{
			{
				"mediaType": oci.MediaTypeOCIImageManifest,
				"digest":    testManifestDigest,
				"size":      512,
			},
			{
				"mediaType": oci.MediaTypeOCIImageManifest,
				"digest":    testConfigDigest,
				"size":      256,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal index body: %v", err)
	}
	indexClient := &stubRegistryClient{
		tags: []string{"multiarch"},
		manifest: distribution.ManifestResponse{
			Digest:    testReferrerDigest,
			MediaType: oci.MediaTypeOCIImageIndex,
			Body:      indexBody,
			SizeBytes: 1024,
		},
	}
	return &Source{
		Config: Config{
			CollectorInstanceID: "oci-parity-8861",
			Targets: []TargetConfig{
				{
					Provider:     oci.ProviderDockerHub,
					Registry:     "registry-1.docker.io",
					Repository:   "library/busybox",
					TagLimit:     10,
					Visibility:   oci.VisibilityPublic,
					AuthMode:     oci.AuthModeAnonymous,
					SourceURI:    "https://registry-1.docker.io/v2/library/busybox",
					FencingToken: 7,
				},
				{
					Provider:     oci.ProviderGHCR,
					Registry:     "ghcr.io",
					Repository:   "eshu-hq/parity-demo",
					TagLimit:     1,
					Visibility:   oci.VisibilityPublic,
					AuthMode:     oci.AuthModeAnonymous,
					SourceURI:    "https://ghcr.io/v2/eshu-hq/parity-demo",
					FencingToken: 9,
				},
				{
					Provider:     oci.ProviderHarbor,
					Registry:     "harbor.example.test",
					Repository:   "team/multiarch",
					TagLimit:     10,
					Visibility:   oci.VisibilityPrivate,
					AuthMode:     oci.AuthModeCredentialed,
					SourceURI:    "https://harbor.example.test/v2/team/multiarch",
					FencingToken: 11,
				},
			},
		},
		ClientFactory: ClientFactoryFunc(func(_ context.Context, target TargetConfig) (RegistryClient, error) {
			switch target.Repository {
			case "eshu-hq/parity-demo":
				return partialClient, nil
			case "team/multiarch":
				return indexClient, nil
			}
			return fullClient, nil
		}),
		Clock: func() time.Time { return parityObservedAt },
	}
}

func drainEnvelopes(collected collector.CollectedGeneration) []facts.Envelope {
	var envelopes []facts.Envelope
	for envelope := range collected.Facts {
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

func kindMultiset(envelopes []facts.Envelope) map[string]int {
	counts := map[string]int{}
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func stableKeys(envelopes []facts.Envelope) []string {
	keys := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		keys = append(keys, envelope.StableFactKey)
	}
	slices.Sort(keys)
	return keys
}

// TestOCI8861ParityLiveAgreesWithCassette is the AC3 source-parity pin: the
// live scan and the checked-in cassette agree scope by scope on scope ID,
// generation ID, fact-kind multiset, stable keys, schema versions, collector
// kind, and fencing tokens. A cutover candidate must reproduce this same
// agreement against the same fixture.
func TestOCI8861ParityLiveAgreesWithCassette(t *testing.T) {
	t.Parallel()

	recorded, err := cassette.NewSource("testdata/8861-parity-oci-registry.json")
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	type snapshot struct {
		scopeID      string
		generationID string
		kinds        map[string]int
		keys         []string
		attrs        map[string]string
		envelopes    []facts.Envelope
	}

	// attrKey binds the host-enforced boundary attributes to the stable key
	// so the cassette pins what admission and grant checks key on.
	attrKey := func(envelope facts.Envelope) string {
		return envelope.StableFactKey + "\x00" + envelope.FactKind + "\x00" +
			envelope.SchemaVersion + "\x00" + strconv.FormatInt(envelope.FencingToken, 10)
	}
	var want []snapshot
	for {
		collected, ok, err := recorded.Next(context.Background())
		if err != nil {
			t.Fatalf("cassette Next() error = %v", err)
		}
		if !ok {
			break
		}
		envelopes := drainEnvelopes(collected)
		if len(envelopes) == 0 {
			t.Fatalf("cassette scope %q has no facts", collected.Scope.ScopeID)
		}
		attrs := map[string]string{}
		for _, envelope := range envelopes {
			attrs[envelope.StableFactKey] = attrKey(envelope)
		}
		want = append(want, snapshot{
			scopeID:      collected.Scope.ScopeID,
			generationID: collected.Generation.GenerationID,
			kinds:        kindMultiset(envelopes),
			keys:         stableKeys(envelopes),
			attrs:        attrs,
			envelopes:    envelopes,
		})
	}
	if len(want) != 3 {
		t.Fatalf("cassette scopes = %d, want 3", len(want))
	}

	live := parityLiveSource(t)
	for i, expected := range want {
		collected, ok, err := live.Next(context.Background())
		if err != nil {
			t.Fatalf("live Next()[%d] error = %v", i, err)
		}
		if !ok {
			t.Fatalf("live Next()[%d] ok = false, want scope %q", i, expected.scopeID)
		}
		if collected.Scope.ScopeID != expected.scopeID {
			t.Fatalf("live ScopeID = %q, want %q", collected.Scope.ScopeID, expected.scopeID)
		}
		if collected.Generation.GenerationID != expected.generationID {
			t.Fatalf("live GenerationID = %q, want %q", collected.Generation.GenerationID, expected.generationID)
		}
		envelopes := drainEnvelopes(collected)
		if !maps.Equal(kindMultiset(envelopes), expected.kinds) {
			t.Fatalf("live kinds = %v, want %v", kindMultiset(envelopes), expected.kinds)
		}
		if !slices.Equal(stableKeys(envelopes), expected.keys) {
			t.Fatalf("live stable keys differ from cassette for scope %q", expected.scopeID)
		}
		gotAttrs := map[string]string{}
		for _, envelope := range envelopes {
			gotAttrs[envelope.StableFactKey] = attrKey(envelope)
		}
		if !maps.Equal(gotAttrs, expected.attrs) {
			t.Fatalf("live boundary attributes differ from cassette for scope %q", expected.scopeID)
		}
		for _, envelope := range envelopes {
			if envelope.SchemaVersion != "1.0.0" {
				t.Fatalf("SchemaVersion = %q, want 1.0.0 for kind %q", envelope.SchemaVersion, envelope.FactKind)
			}
			if envelope.CollectorKind != oci.CollectorKind {
				t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, oci.CollectorKind)
			}
		}
	}
	if _, ok, err := live.Next(context.Background()); err != nil || ok {
		t.Fatalf("live Next() exhaust = (%v, %v), want (false, nil)", ok, err)
	}

	union := map[string]bool{}
	for _, s := range want {
		for kind := range s.kinds {
			union[kind] = true
		}
	}
	for _, kind := range chain.OCIRegistryFactKinds() {
		if !union[kind] {
			t.Fatalf("cassette covers no %q fact; parity pack must span all 7 live kinds", kind)
		}
	}
}

// TestOCI8861ParityDuplicateDeliveryIsStable pins duplicate safety: two
// identical scans emit identical stable-key sets, so a redelivered
// generation commits idempotently at the core boundary.
func TestOCI8861ParityDuplicateDeliveryIsStable(t *testing.T) {
	t.Parallel()

	first, ok, err := parityLiveSource(t).Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("first Next() = (%v, %v), want (true, nil)", ok, err)
	}
	second, ok, err := parityLiveSource(t).Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("second Next() = (%v, %v), want (true, nil)", ok, err)
	}
	if !slices.Equal(stableKeys(drainEnvelopes(first)), stableKeys(drainEnvelopes(second))) {
		t.Fatal("duplicate scans emit different stable keys; redelivery would not be idempotent")
	}
}

// TestOCI8861ParityEmptySuccessorAttestation pins the TrustedDeletion half
// of empty-successor handling: an emptied repository still emits its
// repository envelope, and only an explicit attestation stamps the
// generation trusted. The ingestion store's successor-emptiness backstop
// (core-owned) consumes this bit.
func TestOCI8861ParityEmptySuccessorAttestation(t *testing.T) {
	t.Parallel()

	scan := func(allowEmpty bool) collector.CollectedGeneration {
		t.Helper()
		client := &stubRegistryClient{}
		source := &Source{
			Config: Config{
				CollectorInstanceID: "oci-parity-8861",
				Targets: []TargetConfig{{
					Provider:            oci.ProviderDockerHub,
					Registry:            "registry-1.docker.io",
					Repository:          "library/emptied",
					TagLimit:            10,
					SourceURI:           "https://registry-1.docker.io/v2/library/emptied",
					FencingToken:        7,
					AllowEmptySuccessor: allowEmpty,
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
			t.Fatal("Next() ok = false, want the emptied scope")
		}
		return collected
	}

	plain := scan(false)
	if plain.Generation.TrustedDeletion {
		t.Fatal("TrustedDeletion = true without attestation, want false")
	}
	if kinds := kindMultiset(drainEnvelopes(plain)); kinds[chain.OCIRegistryRepositoryFactKind] != 1 {
		t.Fatalf("emptied scope kinds = %v, want exactly the repository envelope", kinds)
	}
	if attested := scan(true); !attested.Generation.TrustedDeletion {
		t.Fatal("TrustedDeletion = false with AllowEmptySuccessor, want true")
	}
}

// TestOCI8861ParityInTreeNeedsNoGrant documents the deliberate delegation
// delta: the in-tree first-party path emits with no grant lookup, so this
// asserts emission succeeds grant-free. The extracted producer must instead
// fail closed on a missing, expired, or revoked grant (PRODUCER_GRANT_DRAFT);
// the fencing token and collector kind on every envelope are the host-side
// inputs that enforcement keys on.
func TestOCI8861ParityInTreeNeedsNoGrant(t *testing.T) {
	t.Parallel()

	collected, ok, err := parityLiveSource(t).Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want grant-free in-tree emission")
	}
	for _, envelope := range drainEnvelopes(collected) {
		if envelope.CollectorKind != oci.CollectorKind {
			t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, oci.CollectorKind)
		}
		if envelope.FencingToken != 7 {
			t.Fatalf("FencingToken = %d, want 7", envelope.FencingToken)
		}
	}
}
