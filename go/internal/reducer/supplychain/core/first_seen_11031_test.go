// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

// Failing-first proof for eshu-hq/eshu#11031 criterion 6: the durable finding
// first-seen stamp and the steady-state cohort classifier, under the arbiter
// fork ruling (EPSS/KEV excluded from criterion 6; osv/nvd/ghsa/cve_list
// publish instants; set-once stamp; backfill-cohort exclusion).
//
// The stamp is the earliest durable reducer write of a finding identity,
// keyed on the stable fact key (no scope, no generation), server clock at
// write time, never refreshed by rewrite, EPSS/KEV rescore, advisory-group
// re-key, or retraction revival. The cohort classifier turns a
// (published_at, first_seen, T0) triple into an in-cohort sample with its lag
// or a counted exclusion.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// TestFirstSeenCohortClassification11031 pins the steady-state cohort rule:
// a sample enters the cohort only if published_at >= T0 AND the finding
// carries a real stamp. Every other shape is a counted exclusion with a
// named reason, never a zero-valued lag.
func TestFirstSeenCohortClassification11031(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	firstSeen := published.Add(90 * time.Minute)
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	included := ClassifyPublishCohortSample("2026-11-20T10:00:00Z", firstSeen, t0)
	if !included.InCohort {
		t.Fatalf("steady-state sample InCohort = false, reason %q, want included", included.ExclusionReason)
	}
	if included.Lag != 90*time.Minute {
		t.Fatalf("lag = %v, want 90m (first-seen minus publish)", included.Lag)
	}

	// A backfilled historical advisory (published long before T0) is
	// excluded from the steady-state cohort even with a real stamp: it
	// measures fleet age, not publish-to-finding latency.
	backfilled := ClassifyPublishCohortSample("2023-04-11T09:00:00Z", firstSeen, t0)
	if backfilled.InCohort {
		t.Fatalf("backfilled advisory InCohort = true, want excluded")
	}
	if backfilled.ExclusionReason != PublishCohortExclusionBackfill {
		t.Fatalf("backfilled reason = %q, want %q", backfilled.ExclusionReason, PublishCohortExclusionBackfill)
	}

	// A finding with no stamp (written before stamp deployment) is
	// excluded: first-seen is never back-estimated from last-rewrite times.
	unstamped := ClassifyPublishCohortSample("2026-11-20T10:00:00Z", time.Time{}, t0)
	if unstamped.InCohort {
		t.Fatalf("unstamped finding InCohort = true, want excluded")
	}
	if unstamped.ExclusionReason != PublishCohortExclusionNoStamp {
		t.Fatalf("unstamped reason = %q, want %q", unstamped.ExclusionReason, PublishCohortExclusionNoStamp)
	}

	// An empty published_at (real for converted OSV records) excludes the
	// finding from its cohort with a counted reason; it must never be
	// zero-valued into a lag.
	emptyPublish := ClassifyPublishCohortSample("", firstSeen, t0)
	if emptyPublish.InCohort {
		t.Fatalf("empty published_at InCohort = true, want excluded")
	}
	if emptyPublish.ExclusionReason != PublishCohortExclusionUnparseablePublish {
		t.Fatalf("empty publish reason = %q, want %q", emptyPublish.ExclusionReason, PublishCohortExclusionUnparseablePublish)
	}

	// A published_at after first-seen (clock skew, future-dated correction)
	// would poison budget stats with a negative lag: excluded, counted.
	futurePublish := ClassifyPublishCohortSample("2026-11-20T12:00:00Z", firstSeen, t0)
	if futurePublish.InCohort {
		t.Fatalf("publish-after-first-seen InCohort = true, want excluded")
	}
	if futurePublish.ExclusionReason != PublishCohortExclusionPublishAfterFirstSeen {
		t.Fatalf("future publish reason = %q, want %q", futurePublish.ExclusionReason, PublishCohortExclusionPublishAfterFirstSeen)
	}
}

// TestFirstSeenStampSetOnce11031 proves the stamp never moves: a rewrite, an
// EPSS rescore, and an advisory-group re-key of the same stable key all
// resolve to the first claim. The store is a stub; the durable SQL proof
// lives in the disposable-Postgres live test.
func TestFirstSeenStampSetOnce11031(t *testing.T) {
	t.Parallel()

	store := newStubFirstSeenStore()
	firstWrite := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	rewrite := firstWrite.Add(30 * time.Minute)

	claims := []SupplyChainImpactFirstSeenClaim{{StableKey: "supply_chain_impact:CVE-2026-11031:k", PublishSource: "nvd"}}
	first, err := store.ClaimSupplyChainImpactFirstSeen(context.Background(), claims, firstWrite)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if got := first["supply_chain_impact:CVE-2026-11031:k"].FirstSeenAt; !got.Equal(firstWrite) {
		t.Fatalf("first stamp = %v, want write-time %v", got, firstWrite)
	}

	// Rewrite, EPSS rescore, and re-key resolve the same key: the stamp
	// stays at the first write.
	for _, when := range []time.Time{rewrite, rewrite.Add(time.Hour), rewrite.Add(2 * time.Hour)} {
		got, err := store.ClaimSupplyChainImpactFirstSeen(context.Background(), claims, when)
		if err != nil {
			t.Fatalf("re-claim at %v: %v", when, err)
		}
		if stamp := got["supply_chain_impact:CVE-2026-11031:k"].FirstSeenAt; !stamp.Equal(firstWrite) {
			t.Fatalf("stamp moved to %v on re-claim at %v, want set-once %v", stamp, when, firstWrite)
		}
	}
}

// TestFirstSeenOfferPreserved11031 proves a replayed payload keeps its
// original stamp: a claim that offers an earlier valid stamp (cassette
// admission on a fresh database) stores the offered instant, not the replay
// time. An offered instant after now is clock skew and resolves to now.
func TestFirstSeenOfferPreserved11031(t *testing.T) {
	t.Parallel()

	store := newStubFirstSeenStore()
	original := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	replay := original.Add(96 * time.Hour)

	got, err := store.ClaimSupplyChainImpactFirstSeen(context.Background(), []SupplyChainImpactFirstSeenClaim{
		{StableKey: "k-replay", PublishSource: "osv", OfferedAt: original},
	}, replay)
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if stamp := got["k-replay"].FirstSeenAt; !stamp.Equal(original) {
		t.Fatalf("replay stamp = %v, want offered original %v", stamp, original)
	}

	future, err := store.ClaimSupplyChainImpactFirstSeen(context.Background(), []SupplyChainImpactFirstSeenClaim{
		{StableKey: "k-future", PublishSource: "nvd", OfferedAt: replay.Add(time.Hour)},
	}, replay)
	if err != nil {
		t.Fatalf("future-offer claim: %v", err)
	}
	if stamp := future["k-future"].FirstSeenAt; !stamp.Equal(replay) {
		t.Fatalf("future-offer stamp = %v, want clamped to now %v", stamp, replay)
	}
}

// TestFirstSeenNvdPublishedNotModified11031 is the NVD differential the
// ruling requires: on one NVD record the criterion lag anchors on published,
// never on lastModified. lastModified advances on every NVD edit and would
// understate publish lag; NVD collection windows already key on lastModified,
// so a lastModified-based lag would re-measure the planner's own cadence.
func TestFirstSeenNvdPublishedNotModified11031(t *testing.T) {
	t.Parallel()

	published := "2026-11-20T10:00:00Z"
	modified := "2026-11-22T15:30:00Z"
	finding := mustFirstSeenNvdFinding(t, published, modified)

	if finding.AdvisoryPublishedAt != published {
		t.Fatalf("AdvisoryPublishedAt = %q, want NVD published %q (never lastModified %q)",
			finding.AdvisoryPublishedAt, published, modified)
	}
	if finding.AdvisoryUpdatedAt != modified {
		t.Fatalf("AdvisoryUpdatedAt = %q, want NVD lastModified %q", finding.AdvisoryUpdatedAt, modified)
	}
	if finding.AdvisoryPublishSource != "nvd" {
		t.Fatalf("AdvisoryPublishSource = %q, want nvd (the observation that supplied the publish instant)", finding.AdvisoryPublishSource)
	}

	publishInstant, ok := ParsePublishInstant(finding.AdvisoryPublishedAt)
	if !ok {
		t.Fatalf("ParsePublishInstant(%q) failed", finding.AdvisoryPublishedAt)
	}
	modifiedInstant, ok := ParsePublishInstant(finding.AdvisoryUpdatedAt)
	if !ok {
		t.Fatalf("ParsePublishInstant(%q) failed", finding.AdvisoryUpdatedAt)
	}
	firstSeen := publishInstant.Add(90 * time.Minute)
	if lag := firstSeen.Sub(publishInstant); lag != 90*time.Minute {
		t.Fatalf("published-based lag = %v, want 90m", lag)
	}
	if lag := firstSeen.Sub(modifiedInstant); lag == 90*time.Minute {
		t.Fatalf("lastModified-based lag also 90m: the differential collapsed, published=%q modified=%q",
			published, modified)
	}
}

// stubFirstSeenStore is the in-memory SupplyChainImpactFirstSeenStore the
// set-once and offer-preservation proofs run against: it resolves claims the
// way the production SQL does (first writer wins, valid offers preserved)
// without a database.
type stubFirstSeenStore struct {
	stamps map[string]FirstSeenStamp
}

// BenchmarkFirstSeenApply11031 measures the per-pass in-Go cost of
// stamping: two map inserts plus the one marshal the writer already did per
// row, over realistic payloads built by the production encoder (advisory
// sources, severity and range provenance). The SQL claim itself is two
// set-based statements per pass (one INSERT ... SELECT unnest, one SELECT
// ... ANY) no matter how many findings the pass carries, so the marshal is
// the only finding-count-scaled cost and the stamp adds no second
// serialization.
func BenchmarkFirstSeenApply11031(b *testing.B) {
	const rows = 200
	write := SupplyChainImpactWrite{
		IntentID: "intent:11031:bench", ScopeID: "scope:bench", GenerationID: "gen:bench",
		SourceSystem: "vulnerability_intelligence", Cause: "synthetic #11031 bench",
	}
	finding := SupplyChainImpactFinding{
		CVEID: "CVE-2026-11031", AdvisoryID: "CVE-2026-11031",
		PackageID: testImpactPackageID, Ecosystem: "npm", PackageName: "example",
		ObservedVersion: "1.2.3", FixedVersion: "1.3.0", Status: SupplyChainImpactAffectedExact,
		CVSSScore: 7.5, SeveritySource: "nvd",
		AdvisoryPublishedAt: "2026-11-20T10:00:00Z", AdvisoryUpdatedAt: "2026-11-21T15:30:00Z",
		AdvisoryPublishSource: "nvd",
		AdvisorySources: []AdvisorySourceObservation{
			{Source: "nvd", AdvisoryID: "CVE-2026-11031", SourceUpdatedAt: "2026-11-21T15:30:00Z"},
			{Source: "ghsa", AdvisoryID: "GHSA-xxxx-yyyy-zzzz"},
		},
	}
	versioned := make([]factwrite.VersionedRow, rows)
	payloads := make([]map[string]any, rows)
	stamps := make(map[string]FirstSeenStamp, rows)
	now := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	for i := range versioned {
		key := "supply_chain_impact:CVE-2026-11031:k" + string(rune('a'+i%26)) + string(rune('0'+i%10))
		versioned[i] = factwrite.VersionedRow{FactID: "fact-" + key, StableFactKey: key}
		payloads[i] = supplyChainImpactPayload(write, finding)
		stamps[key] = FirstSeenStamp{FirstSeenAt: now, PublishSource: "nvd"}
	}
	sized, err := json.Marshal(payloads[0])
	if err != nil {
		b.Fatalf("measure fixture payload: %v", err)
	}
	b.Logf("fixture payload bytes = %d", len(sized))
	clonePayloads := func() []map[string]any {
		maps := make([]map[string]any, rows)
		for j := range payloads {
			clone := make(map[string]any, len(payloads[j]))
			for k, v := range payloads[j] {
				clone[k] = v
			}
			maps[j] = clone
		}
		return maps
	}
	b.Run("stamped", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cp := make([]factwrite.VersionedRow, rows)
			copy(cp, versioned)
			if err := marshalStampedPayloads(cp, clonePayloads(), stamps); err != nil {
				b.Fatalf("marshal: %v", err)
			}
		}
	})
	// Unstamped is the pre-stamp build path (the same single marshal with no
	// stamp keys): the sub-benchmark delta is the stamp's marginal cost.
	b.Run("unstamped", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		empty := map[string]FirstSeenStamp{}
		for i := 0; i < b.N; i++ {
			cp := make([]factwrite.VersionedRow, rows)
			copy(cp, versioned)
			if err := marshalStampedPayloads(cp, clonePayloads(), empty); err != nil {
				b.Fatalf("marshal: %v", err)
			}
		}
	})
}

func newStubFirstSeenStore() *stubFirstSeenStore {
	return &stubFirstSeenStore{stamps: map[string]FirstSeenStamp{}}
}

// ClaimSupplyChainImpactFirstSeen resolves claims the way the production SQL
// does: the first writer of a key wins (its offer resolved against its now),
// every later claim of the same key resolves to the stored stamp.
func (s *stubFirstSeenStore) ClaimSupplyChainImpactFirstSeen(_ context.Context, claims []SupplyChainImpactFirstSeenClaim, now time.Time) (map[string]FirstSeenStamp, error) {
	out := make(map[string]FirstSeenStamp, len(claims))
	for _, claim := range claims {
		if stamp, ok := s.stamps[claim.StableKey]; ok {
			out[claim.StableKey] = stamp
			continue
		}
		stamp := FirstSeenStamp{
			FirstSeenAt:   ResolveFirstSeenOffer(claim.OfferedAt, now),
			PublishSource: claim.PublishSource,
		}
		s.stamps[claim.StableKey] = stamp
		out[claim.StableKey] = stamp
	}
	return out, nil
}

// mustFirstSeenNvdFinding builds one package-path finding from a single NVD
// observation with distinct published and lastModified instants.
func mustFirstSeenNvdFinding(t *testing.T, published, modified string) SupplyChainImpactFinding {
	t.Helper()

	const cveID = "CVE-2026-11031"
	cve := vulnerabilityCVEFactWithDates("cve-first-seen-11031", cveID, 7.5, "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", published, modified)
	cve.Payload["source"] = "nvd"
	envelopes := []facts.Envelope{
		cve,
		vulnerabilityAffectedPackageFact("affected-first-seen-11031", cveID, testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
		packageConsumptionFactWithRange("consume-first-seen-11031", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
	}
	findings := BuildSupplyChainImpactFindings(envelopes)
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	return findings[0]
}
