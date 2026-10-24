// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gcpruntime parity pin for #8856 (AC3, in-tree side).
//
// These tests prove the CURRENT in-tree source behavior that the #4047
// cutover gate must reproduce exactly: the same scopes, kinds, stable keys,
// fencing tokens, and in-band failure signals, whether the facts come from a
// live scan or from the recorded fixture pack
// (testdata/8856-parity-gcp-cloud.json, cut from a live run with fixed
// clock, generation IDs, and redaction key). The #6710 compatibility harness
// reuses this pack: the live side is the oracle, the fixture is the recorded
// expectation.
//
// Out of scope here (core-owned, recorded for the external producer):
// grant revocation enforcement (see PRODUCER_GRANT_DRAFT.md) and durable
// commit/lease semantics owned by the ingestion store and workflow control
// store. The in-tree path pins the emission half; the external path must
// additionally fail closed on missing, expired, or revoked grants.
//
// Known delegation deltas pinned (not fixed) by this pack:
//   - mirror envelopes (gcp_iam_principal, gcp_iam_permission_policy,
//     gcp_iam_trust_policy) carry collector_kind "secrets_iam_posture", not
//     "gcp", because the posture envelope builder stamps its own kind.
//   - the producer record for those mirror kinds names "secrets-iam-posture"
//     (go/internal/facts/identity/iam.go), not "collector-gcp-cloud".
// The cutover grant must reconcile both before production cutover.

package gcpruntime

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

var (
	parity8856UpdateAt = time.Date(2026, 6, 9, 11, 50, 0, 0, time.UTC)
	parity8856ReadAt   = time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
)

// parity8856Provider serves the representative fixture pages and scripted
// hostile outcomes per scope: a dangling continuation token for the expired
// scope and a typed provider warning for the quota scope.
type parity8856Provider struct {
	fixture *FixturePageProvider
	warning map[string]ProviderWarning
}

func (p *parity8856Provider) FetchPage(ctx context.Context, req PageRequest) (gcpcloud.AssetsListPage, error) {
	if warning, ok := p.warning[req.Scope.ScopeID]; ok {
		return gcpcloud.AssetsListPage{}, warning
	}
	return p.fixture.FetchPage(ctx, req)
}

// parity8856Scopes builds the three pack scopes: one representative mix that
// spans every emitted fact family, one page-token-expiry hostile, and one
// quota-warning hostile. Generation IDs are explicit so the pack is stable
// independent of the clock.
func parity8856Scopes() (representative, expired, quota ScopeConfig) {
	representative = ScopeConfig{
		ParentScopeKind: catalog.ParentScopeProject,
		ParentScopeID:   "parity-proj",
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		GenerationID:    "gen-8856-representative",
		FencingToken:    7,
		CredentialRef:   "parity-readonly",
	}
	expired = ScopeConfig{
		ParentScopeKind: catalog.ParentScopeProject,
		ParentScopeID:   "expired-proj",
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		GenerationID:    "gen-8856-expired",
		FencingToken:    9,
		CredentialRef:   "parity-readonly",
	}
	quota = ScopeConfig{
		ParentScopeKind: catalog.ParentScopeProject,
		ParentScopeID:   "quota-proj",
		AssetTypeFamily: "mixed",
		ContentFamily:   "resource",
		LocationBucket:  "global",
		GenerationID:    "gen-8856-quota",
		FencingToken:    11,
		CredentialRef:   "parity-readonly",
	}
	return representative, expired, quota
}

// parity8856LiveSource rebuilds the exact source the fixture pack was cut
// from: same scopes, same pages, same fixed clock and redaction key, so
// generation IDs and stable keys reproduce deterministically.
func parity8856LiveSource(t *testing.T) *Source {
	t.Helper()
	representative, expired, quota := parity8856Scopes()
	resolved := func(s ScopeConfig) ScopeConfig { return s.withDefaults() }
	pagesByScope := map[string][]gcpcloud.AssetsListPage{
		resolved(representative).ScopeID: {
			{ReadTime: parity8856ReadAt, Resources: parity8856PageOne(), NextPageToken: "PARITY-P2"},
			{ReadTime: parity8856ReadAt, Resources: parity8856PageTwo()},
		},
		// A single page whose continuation token dangles: resuming it is a
		// page-token-expired warning, not silent truncation.
		resolved(expired).ScopeID: {
			{ReadTime: parity8856ReadAt, Resources: parity8856ExpiredPage(), NextPageToken: "DANGLING"},
		},
	}
	return &Source{
		Config: Config{
			CollectorInstanceID: "gcp-parity-8856",
			PollInterval:        time.Minute,
			Scopes:              []ScopeConfig{representative, expired, quota},
		},
		Provider: &parity8856Provider{
			fixture: NewFixturePageProvider(pagesByScope),
			warning: map[string]ProviderWarning{
				resolved(quota).ScopeID: {
					WarningKind: catalog.WarningKindQuota,
					Outcome:     catalog.OutcomeUnavailable,
					Reason:      "CAI quota exhausted for scope",
					Retryable:   true,
					SourceURI:   "cai://assets.list",
				},
			},
		},
		RedactionKey: testRedactionKey(t),
		Tracker:      gcpcloud.NewGenerationTracker(),
		Metrics:      testMetrics(t),
		Clock:        fixedClock(),
	}
}

func parity8856PageOne() []gcpcloud.ResourceObservation {
	return []gcpcloud.ResourceObservation{
		{
			Name:        "//compute.googleapis.com/projects/parity-proj/zones/us-central1-a/instances/parity-vm",
			AssetType:   "compute.googleapis.com/Instance",
			DisplayName: "parity-vm",
			State:       "RUNNING",
			Location:    "us-central1-a",
			Ancestors:   []string{"projects/424242", "organizations/9988776655"},
			Labels:      map[string]string{"env": "prod"},
			IAMPolicyBindings: []gcpcloud.IAMPolicyBindingObservation{{
				Role:    "roles/compute.viewer",
				Members: []string{"serviceAccount:reader@parity-proj.iam.gserviceaccount.com"},
			}},
			Relationships: []gcpcloud.RelationshipObservation{{
				RelationshipType:       "USES",
				TargetFullResourceName: "//storage.googleapis.com/projects/_/buckets/parity-bucket",
				TargetAssetType:        "storage.googleapis.com/Bucket",
			}},
			UpdateTime: parity8856UpdateAt,
			SourceURI:  "cai://assets.list/parity",
		},
		{
			Name:        "//storage.googleapis.com/projects/_/buckets/parity-bucket",
			AssetType:   "storage.googleapis.com/Bucket",
			DisplayName: "parity-bucket",
			Location:    "us-central1",
			Ancestors:   []string{"projects/424242", "organizations/9988776655"},
			Labels:      map[string]string{"team": "data"},
			UpdateTime:  parity8856UpdateAt,
			SourceURI:   "cai://assets.list/parity",
		},
	}
}

func parity8856PageTwo() []gcpcloud.ResourceObservation {
	return []gcpcloud.ResourceObservation{
		{
			Name:      "//dns.googleapis.com/projects/parity-proj/managedZones/parity-zone",
			AssetType: "dns.googleapis.com/ManagedZone",
			Location:  "global",
			Ancestors: []string{"projects/424242", "organizations/9988776655"},
			DNSRecords: []gcpcloud.DNSRecordObservation{{
				ManagedZoneFullResourceName: "//dns.googleapis.com/projects/parity-proj/managedZones/parity-zone",
				RecordType:                  "A",
				RecordName:                  "www.example.test.",
				Targets:                     []string{"203.0.113.10"},
				TTLSeconds:                  300,
			}},
			UpdateTime: parity8856UpdateAt,
			SourceURI:  "cai://assets.list/parity",
		},
		{
			Name:      "//run.googleapis.com/projects/parity-proj/locations/us-central1/services/parity-svc",
			AssetType: "run.googleapis.com/Service",
			Location:  "us-central1",
			Ancestors: []string{"projects/424242", "organizations/9988776655"},
			ImageReferences: []gcpcloud.ImageReferenceObservation{{
				ImageReference: "us-central1-docker.pkg.dev/parity-proj/repo/svc:1.2.3",
				ImageDigest:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ContainerName:  "parity-web-sidecar",
			}},
			UpdateTime: parity8856UpdateAt,
			SourceURI:  "cai://assets.list/parity",
		},
		{
			Name:                "//iam.googleapis.com/projects/parity-proj/serviceAccounts/deployer@parity-proj.iam.gserviceaccount.com",
			AssetType:           catalog.ServiceAccountAssetType,
			Ancestors:           []string{"projects/424242", "organizations/9988776655"},
			ServiceAccountEmail: "deployer@parity-proj.iam.gserviceaccount.com",
			IAMPolicyBindings: []gcpcloud.IAMPolicyBindingObservation{{
				Role:    "roles/iam.serviceAccountTokenCreator",
				Members: []string{"serviceAccount:ci@parity-proj.iam.gserviceaccount.com"},
			}},
			UpdateTime: parity8856UpdateAt,
			SourceURI:  "cai://assets.list/parity",
		},
	}
}

func parity8856ExpiredPage() []gcpcloud.ResourceObservation {
	return []gcpcloud.ResourceObservation{
		{
			Name:        "//storage.googleapis.com/projects/_/buckets/expired-bucket",
			AssetType:   "storage.googleapis.com/Bucket",
			DisplayName: "expired-bucket",
			Location:    "us-central1",
			Ancestors:   []string{"projects/777", "organizations/9988776655"},
			Labels:      map[string]string{"team": "expired"},
			UpdateTime:  parity8856UpdateAt,
			SourceURI:   "cai://assets.list/expired",
		},
	}
}

// parity8856Snapshot is the recorded expectation: per scope, the scope and
// generation identity plus the kind multiset, sorted stable keys, and the
// host-enforced boundary attributes bound to each key.
type parity8856Snapshot struct {
	Scopes []parity8856ScopeSnapshot `json:"scopes"`
}

type parity8856ScopeSnapshot struct {
	ScopeID      string            `json:"scope_id"`
	GenerationID string            `json:"generation_id"`
	Kinds        map[string]int    `json:"kinds"`
	Keys         []string          `json:"keys"`
	Attrs        map[string]string `json:"attrs"`
}

// attr8856Key binds the host-enforced boundary attributes to the stable key
// so the pack pins what admission and grant checks key on.
func attr8856Key(envelope facts.Envelope) string {
	return envelope.StableFactKey + "\x00" + envelope.FactKind + "\x00" +
		envelope.SchemaVersion + "\x00" + envelope.CollectorKind + "\x00" +
		string(envelope.SourceConfidence) + "\x00" + strconv.FormatInt(envelope.FencingToken, 10)
}

func snapshot8856Scope(collected collector.CollectedGeneration, envelopes []facts.Envelope) parity8856ScopeSnapshot {
	attrs := make(map[string]string, len(envelopes))
	for _, envelope := range envelopes {
		attrs[envelope.StableFactKey] = attr8856Key(envelope)
	}
	return parity8856ScopeSnapshot{
		ScopeID:      collected.Scope.ScopeID,
		GenerationID: collected.Generation.GenerationID,
		Kinds:        kind8856Multiset(envelopes),
		Keys:         stable8856Keys(envelopes),
		Attrs:        attrs,
	}
}

func kind8856Multiset(envelopes []facts.Envelope) map[string]int {
	counts := map[string]int{}
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func stable8856Keys(envelopes []facts.Envelope) []string {
	keys := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		keys = append(keys, envelope.StableFactKey)
	}
	slices.Sort(keys)
	return keys
}

func load8856Snapshot(t *testing.T) parity8856Snapshot {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "8856-parity-gcp-cloud.json"))
	if err != nil {
		t.Fatalf("read parity pack: %v", err)
	}
	var snapshot parity8856Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("parse parity pack: %v", err)
	}
	return snapshot
}

// TestGCP8856ParityLiveAgreesWithFixturePack is the AC3 source-parity pin:
// the live scan and the checked-in pack agree scope by scope on scope ID,
// generation ID, fact-kind multiset, stable keys, schema versions, collector
// kinds, confidence, and fencing tokens. A cutover candidate must reproduce
// this same agreement against the same fixture.
func TestGCP8856ParityLiveAgreesWithFixturePack(t *testing.T) {
	t.Parallel()

	want := load8856Snapshot(t)
	if len(want.Scopes) != 3 {
		t.Fatalf("pack scopes = %d, want 3", len(want.Scopes))
	}

	live := parity8856LiveSource(t)
	for i, expected := range want.Scopes {
		collected, ok, err := live.Next(context.Background())
		if err != nil {
			t.Fatalf("live Next()[%d] error = %v", i, err)
		}
		if !ok {
			t.Fatalf("live Next()[%d] ok = false, want scope %q", i, expected.ScopeID)
		}
		if collected.Scope.ScopeID != expected.ScopeID {
			t.Fatalf("live ScopeID = %q, want %q", collected.Scope.ScopeID, expected.ScopeID)
		}
		if collected.Generation.GenerationID != expected.GenerationID {
			t.Fatalf("live GenerationID = %q, want %q", collected.Generation.GenerationID, expected.GenerationID)
		}
		envelopes := drainFacts(t, collected)
		if len(envelopes) == 0 {
			t.Fatalf("live scope %q has no facts", expected.ScopeID)
		}
		got := snapshot8856Scope(collected, envelopes)
		if !maps.Equal(got.Kinds, expected.Kinds) {
			t.Fatalf("live kinds = %v, want %v", got.Kinds, expected.Kinds)
		}
		if !slices.Equal(got.Keys, expected.Keys) {
			t.Fatalf("live stable keys differ from pack for scope %q", expected.ScopeID)
		}
		if !maps.Equal(got.Attrs, expected.Attrs) {
			t.Fatalf("live boundary attributes differ from pack for scope %q", expected.ScopeID)
		}
		for _, envelope := range envelopes {
			if envelope.SourceConfidence != facts.SourceConfidenceReported {
				t.Fatalf("SourceConfidence = %q, want reported for kind %q", envelope.SourceConfidence, envelope.FactKind)
			}
			if envelope.SchemaVersion == "" {
				t.Fatalf("empty SchemaVersion for kind %q", envelope.FactKind)
			}
		}
	}
	if _, ok, err := live.Next(context.Background()); err != nil || ok {
		t.Fatalf("live Next() exhaust = (%v, %v), want (false, nil)", ok, err)
	}

	union := map[string]bool{}
	for _, s := range want.Scopes {
		for kind := range s.Kinds {
			union[kind] = true
		}
	}
	for _, kind := range cloud.GCPFactKinds() {
		if !union[kind] {
			t.Fatalf("pack covers no %q fact; parity pack must span all 7 live kinds", kind)
		}
	}
}

// TestGCP8856ParityDuplicateDeliveryIsStable pins duplicate safety: two
// identical scans emit identical stable-key sets, so a redelivered
// generation commits idempotently at the core boundary.
func TestGCP8856ParityDuplicateDeliveryIsStable(t *testing.T) {
	t.Parallel()

	first, ok, err := parity8856LiveSource(t).Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("first Next() = (%v, %v), want (true, nil)", ok, err)
	}
	second, ok, err := parity8856LiveSource(t).Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("second Next() = (%v, %v), want (true, nil)", ok, err)
	}
	if !slices.Equal(stable8856Keys(drainFacts(t, first)), stable8856Keys(drainFacts(t, second))) {
		t.Fatal("duplicate scans emit different stable keys; redelivery would not be idempotent")
	}
}

// TestGCP8856ParityStaleGenerationEmitsWarningOnly pins fencing: a scope
// already owned by a newer fencing token emits a single stale warning and
// no resource facts, so a stale scan can neither replace nor drop current
// facts.
func TestGCP8856ParityStaleGenerationEmitsWarningOnly(t *testing.T) {
	t.Parallel()

	representative, _, _ := parity8856Scopes()
	resolved := representative.withDefaults()
	source := parity8856LiveSource(t)
	if err := source.Tracker.Accept(resolved.ScopeID, "gen-8856-newer", resolved.FencingToken+1); err != nil {
		t.Fatalf("pre-accept newer token: %v", err)
	}
	collected, ok, err := source.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("stale Next() = (%v, %v), want (true, nil)", ok, err)
	}
	envelopes := drainFacts(t, collected)
	if len(envelopes) != 1 {
		t.Fatalf("stale envelopes = %d, want exactly the stale warning", len(envelopes))
	}
	if envelopes[0].FactKind != cloud.GCPCollectionWarningFactKind {
		t.Fatalf("stale kind = %q, want %q", envelopes[0].FactKind, cloud.GCPCollectionWarningFactKind)
	}
}

// TestGCP8856ParityRedactionKeepsRawIdentityOutOfPayloads pins the parser
// choke point: raw IAM members, DNS names and targets, and container names
// never reach durable payloads. Verbatim provider identity (full resource
// names) and non-sensitive label values stay by design and are not in the
// blocklist.
func TestGCP8856ParityRedactionKeepsRawIdentityOutOfPayloads(t *testing.T) {
	t.Parallel()

	blocklist := []string{
		"reader@parity-proj.iam.gserviceaccount.com",
		"ci@parity-proj.iam.gserviceaccount.com",
		"deployer@parity-proj.iam.gserviceaccount.com",
		"www.example.test.",
		"203.0.113.10",
		"parity-web-sidecar",
	}
	source := parity8856LiveSource(t)
	for {
		collected, ok, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		if !ok {
			break
		}
		for _, envelope := range drainFacts(t, collected) {
			// full_resource_name is verbatim provider identity preserved by
			// design for exact reducer joins (for example a ServiceAccount
			// name embeds its email); it is not a redaction leak.
			payload := maps.Clone(envelope.Payload)
			delete(payload, "full_resource_name")
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			for _, blocked := range blocklist {
				if strings.Contains(string(raw), blocked) {
					t.Fatalf("payload for kind %q leaks %q", envelope.FactKind, blocked)
				}
			}
		}
	}
}

// TestGCP8856ParityInTreeNeedsNoGrant documents the deliberate delegation
// delta: the in-tree first-party path emits with no grant lookup, so this
// asserts emission succeeds grant-free. The extracted producer must instead
// fail closed on a missing, expired, or revoked grant (PRODUCER_GRANT_DRAFT);
// the fencing token on every envelope is the host-side input that
// enforcement keys on.
func TestGCP8856ParityInTreeNeedsNoGrant(t *testing.T) {
	t.Parallel()

	representative, _, _ := parity8856Scopes()
	wantFencing := representative.FencingToken
	collected, ok, err := parity8856LiveSource(t).Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want grant-free in-tree emission")
	}
	for _, envelope := range drainFacts(t, collected) {
		if envelope.FencingToken != wantFencing {
			t.Fatalf("FencingToken = %d, want %d", envelope.FencingToken, wantFencing)
		}
	}
}
