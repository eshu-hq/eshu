// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"
	"time"
)

// Failing-first proof for eshu-hq/eshu#11466 (case g: classifier units).
// The per-value enrichment cohort classifier decides whether one
// (canonical_key, enrichment_source, value_identity) stamp enters the
// steady-state enrichment-arrival-to-rescore cohort. A sample enters only
// with a known source, a non-empty value identity, a real first-carry
// instant, a real ingestion anchor, first-carry at or after ingest, and an
// ingest at or after the per-source T0. The bucket names the per-value
// cohort (source + value), so KEV flips land in distinct buckets and EPSS
// rescores never pool across score_dates. No budget lives here: QA sets
// budgets after arbiter-reviewed measurement over >= 2 real cycles per
// source.
func TestClassifyEnrichmentCarrySample11466(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	ingest := time.Date(2026, time.November, 20, 10, 0, 0, 0, time.UTC)
	carried := ingest.Add(42 * time.Second)

	t.Run("epss rescore in cohort with finite lag", func(t *testing.T) {
		t.Parallel()
		verdict := ClassifyEnrichmentCarrySample(EnrichmentSourceFirstEPSS, "2026-05-17", carried, ingest, t0)
		if !verdict.InCohort {
			t.Fatalf("verdict = %+v, want in-cohort", verdict)
		}
		if verdict.Lag != 42*time.Second {
			t.Fatalf("lag = %v, want 42s", verdict.Lag)
		}
		if verdict.Bucket != "first_epss:2026-05-17" {
			t.Fatalf("bucket = %q, want per-value first_epss:2026-05-17", verdict.Bucket)
		}
	})

	t.Run("kev true and false land in distinct flip buckets", func(t *testing.T) {
		t.Parallel()
		listed := ClassifyEnrichmentCarrySample(EnrichmentSourceCISAKEV, "true", carried, ingest, t0)
		delisted := ClassifyEnrichmentCarrySample(EnrichmentSourceCISAKEV, "false", carried.Add(time.Hour), ingest, t0)
		if !listed.InCohort || !delisted.InCohort {
			t.Fatalf("flip verdicts = %+v / %+v, want both in-cohort", listed, delisted)
		}
		if listed.Bucket == delisted.Bucket {
			t.Fatalf("flip buckets collide at %q, want distinct per-value buckets", listed.Bucket)
		}
	})

	t.Run("sources never pool", func(t *testing.T) {
		t.Parallel()
		epss := ClassifyEnrichmentCarrySample(EnrichmentSourceFirstEPSS, "true", carried, ingest, t0)
		kev := ClassifyEnrichmentCarrySample(EnrichmentSourceCISAKEV, "true", carried, ingest, t0)
		if epss.Bucket == kev.Bucket {
			t.Fatalf("cross-source buckets collide at %q", epss.Bucket)
		}
	})

	t.Run("unknown source excluded", func(t *testing.T) {
		t.Parallel()
		verdict := ClassifyEnrichmentCarrySample("osv", "2026-05-17", carried, ingest, t0)
		if verdict.InCohort || verdict.ExclusionReason != EnrichmentCohortExclusionUnknownSource {
			t.Fatalf("verdict = %+v, want unknown-source exclusion", verdict)
		}
	})

	t.Run("empty value identity excluded", func(t *testing.T) {
		t.Parallel()
		verdict := ClassifyEnrichmentCarrySample(EnrichmentSourceFirstEPSS, "", carried, ingest, t0)
		if verdict.InCohort || verdict.ExclusionReason != EnrichmentCohortExclusionUnknownValue {
			t.Fatalf("verdict = %+v, want unknown-value exclusion", verdict)
		}
	})

	t.Run("zero first-carry excluded as no stamp", func(t *testing.T) {
		t.Parallel()
		verdict := ClassifyEnrichmentCarrySample(EnrichmentSourceFirstEPSS, "2026-05-17", time.Time{}, ingest, t0)
		if verdict.InCohort || verdict.ExclusionReason != EnrichmentCohortExclusionNoStamp {
			t.Fatalf("verdict = %+v, want no-stamp exclusion", verdict)
		}
	})

	t.Run("anchorless sample excluded", func(t *testing.T) {
		t.Parallel()
		// A KEV-false-by-default carry has no KEV fact, so the QA join
		// yields no ingestion anchor: it is observed in its flip bucket
		// shape but never lag-measured.
		verdict := ClassifyEnrichmentCarrySample(EnrichmentSourceCISAKEV, "false", carried, time.Time{}, t0)
		if verdict.InCohort || verdict.ExclusionReason != EnrichmentCohortExclusionNoIngestAnchor {
			t.Fatalf("verdict = %+v, want no-ingest-anchor exclusion", verdict)
		}
	})

	t.Run("carried before ingest excluded", func(t *testing.T) {
		t.Parallel()
		verdict := ClassifyEnrichmentCarrySample(EnrichmentSourceFirstEPSS, "2026-05-17", ingest.Add(-time.Second), ingest, t0)
		if verdict.InCohort || verdict.ExclusionReason != EnrichmentCohortExclusionCarriedBeforeIngest {
			t.Fatalf("verdict = %+v, want carried-before-ingest exclusion", verdict)
		}
	})

	t.Run("backfilled ingest excluded", func(t *testing.T) {
		t.Parallel()
		old := time.Date(2023, time.April, 11, 9, 0, 0, 0, time.UTC)
		verdict := ClassifyEnrichmentCarrySample(EnrichmentSourceFirstEPSS, "2023-04-11", old.Add(time.Minute), old, t0)
		if verdict.InCohort || verdict.ExclusionReason != EnrichmentCohortExclusionBackfill {
			t.Fatalf("verdict = %+v, want backfill exclusion", verdict)
		}
	})

	t.Run("seeded-clock re-ingest stays measurable", func(t *testing.T) {
		t.Parallel()
		// Disposable-proof clocks run ahead of every seeded generation:
		// the original carry postdates the re-ingest generation, so the
		// join arithmetic stays positive and in-cohort. Production runs
		// the re-ingest generation after the previous resweep (carried
		// before ingest, excluded above): the live pass asserts the stamp
		// never moves and the anchor resolves to the new generation; the
		// exclusion rule itself lives in "carried before ingest excluded".
		olderIngest := ingest.Add(-time.Hour)
		verdict := ClassifyEnrichmentCarrySample(EnrichmentSourceFirstEPSS, "2026-05-17", carried, olderIngest, t0)
		if !verdict.InCohort || verdict.Lag != carried.Sub(olderIngest) {
			t.Fatalf("verdict = %+v, want in-cohort with lag carried-minus-ingest", verdict)
		}
	})

	t.Run("value identity rides the verdict", func(t *testing.T) {
		t.Parallel()
		verdict := ClassifyEnrichmentCarrySample(EnrichmentSourceFirstEPSS, "2026-05-17", carried, ingest, t0)
		if verdict.ValueIdentity != "2026-05-17" {
			t.Fatalf("value identity = %q, want the stamped score_date", verdict.ValueIdentity)
		}
	})
}
