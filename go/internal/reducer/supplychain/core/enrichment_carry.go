// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"time"
)

// This file owns the per-value enrichment cohort classifier for
// eshu-hq/eshu#11466 (enrichment-arrival-to-rescore for first_epss and
// cisa_kev; part of #9209), under the arbiter fork ruling recorded on the
// issue: a separate stamp table keyed (canonical_key, enrichment_source)
// records, set-once per value, the first instant the winners-visible row
// carries the new EPSS score_date (or KnownExploited) value. This classifier
// is pure and has no wire surface: the QA harness supplies the stamp row and
// the ingestion anchor, this function only judges.
//
// Contract classification: additive-optional minor. Neither source pools
// with the other or with the #11031 publish-to-finding cohort (EPSS/KEV
// neither create findings nor carry publish instants), and no payload, SDK,
// or factschema change backs it: the stamp reads the winner payload's
// existing selected_epss_score_date provenance and known_exploited fields.
// No budget lives here: budgets return to the arbiter only after QA measures
// p50/p95/max per source over >= 2 real cycles per source plus arbiter
// review.
//
// Decision D1 (for arbiter review): value_identity is the EPSS score_date
// for first_epss and "true"/"false" for cisa_kev. A KEV-false-by-default
// carry has no KEV fact, so its QA join yields no ingestion anchor and the
// classifier excludes it (no-ingest-anchor) while the mechanism still
// records the false episode, so a later flip back to true moves the stamp
// observably. The KEV flip bucket is the per-value bucket pair
// cisa_kev:true vs cisa_kev:false, never a pooled KEV cohort.

// Enrichment sources measured for arrival-to-rescore. The closed set is the
// arbiter's separation rule: advisory-publishing sources belong to #11031
// criterion 6, never here, and the two enrichment sources never pool.
const (
	// EnrichmentSourceFirstEPSS is the EPSS rescore feed: the carried
	// value is the selected EPSS score_date (a whole-universe rescore day
	// identifies its values).
	EnrichmentSourceFirstEPSS = "first_epss"
	// EnrichmentSourceCISAKEV is the CISA KEV feed: the carried value is
	// the winners-visible KnownExploited flag.
	EnrichmentSourceCISAKEV = "cisa_kev"
)

// Enrichment cohort exclusion reasons. Each names a counted QA exclusion so
// a thin cohort cannot masquerade as a budget.
const (
	// EnrichmentCohortExclusionUnknownSource marks a sample naming a
	// source outside the measured enrichment pair (a defense-in-depth
	// verdict: the stamp table CHECK already refuses such rows).
	EnrichmentCohortExclusionUnknownSource = "unknown-source"
	// EnrichmentCohortExclusionUnknownValue marks an empty value
	// identity (no EPSS selected, nothing carried to measure).
	EnrichmentCohortExclusionUnknownValue = "unknown-value"
	// EnrichmentCohortExclusionNoStamp marks a zero first-carry instant:
	// the winners row carries a value the stamp table never recorded
	// (written before stamp deployment). First-carried is never
	// back-estimated.
	EnrichmentCohortExclusionNoStamp = "no-stamp"
	// EnrichmentCohortExclusionNoIngestAnchor marks a sample whose QA
	// join found no ingestion anchor: a KEV-false-by-default carry has no
	// KEV fact, so there is no generation ingested_at to measure from.
	EnrichmentCohortExclusionNoIngestAnchor = "no-ingest-anchor"
	// EnrichmentCohortExclusionCarriedBeforeIngest marks a first-carry
	// instant before the ingestion anchor (clock skew, future-dated
	// correction), which would otherwise poison budget stats with a
	// negative lag.
	EnrichmentCohortExclusionCarriedBeforeIngest = "carried-before-ingest"
	// EnrichmentCohortExclusionBackfill marks a sample ingested before
	// the per-source steady-state T0: it measures fleet age, not
	// enrichment-arrival-to-rescore latency.
	EnrichmentCohortExclusionBackfill = "backfill-before-t0"
)

// EnrichmentCarryVerdict is the steady-state cohort decision for one
// stamped enrichment value: either an in-cohort sample with its lag
// (first-carried minus ingest) in its per-value bucket, or an exclusion
// with its counted reason.
type EnrichmentCarryVerdict struct {
	InCohort        bool
	ExclusionReason string
	Lag             time.Duration
	// Bucket names the per-value cohort: source + ":" + value_identity
	// (for example "first_epss:2026-05-17" or "cisa_kev:true"), so KEV
	// flips land in distinct buckets and rescores never pool.
	Bucket string
	// ValueIdentity echoes the stamped value the verdict judged.
	ValueIdentity string
}

// ClassifyEnrichmentCarrySample decides whether one stamped enrichment
// value enters the steady-state enrichment-arrival-to-rescore cohort. A
// sample enters only with a known source, a non-empty value identity, a
// real first-carry instant, a real ingestion anchor, first-carry at or
// after ingest, and ingest at or after t0 (the per-source T0 the QA harness
// supplies; this function only judges). Pure: every input arrives as an
// argument, so the rule is unit-provable without a database.
func ClassifyEnrichmentCarrySample(source, valueIdentity string, firstCarriedAt, tIngest, t0 time.Time) EnrichmentCarryVerdict {
	verdict := EnrichmentCarryVerdict{ValueIdentity: valueIdentity}
	switch source {
	case EnrichmentSourceFirstEPSS, EnrichmentSourceCISAKEV:
		verdict.Bucket = source + ":" + valueIdentity
	default:
		verdict.ExclusionReason = EnrichmentCohortExclusionUnknownSource
		return verdict
	}
	if valueIdentity == "" {
		verdict.ExclusionReason = EnrichmentCohortExclusionUnknownValue
		return verdict
	}
	if firstCarriedAt.IsZero() {
		verdict.ExclusionReason = EnrichmentCohortExclusionNoStamp
		return verdict
	}
	if tIngest.IsZero() {
		verdict.ExclusionReason = EnrichmentCohortExclusionNoIngestAnchor
		return verdict
	}
	if firstCarriedAt.Before(tIngest) {
		verdict.ExclusionReason = EnrichmentCohortExclusionCarriedBeforeIngest
		return verdict
	}
	if tIngest.Before(t0) {
		verdict.ExclusionReason = EnrichmentCohortExclusionBackfill
		return verdict
	}
	verdict.InCohort = true
	verdict.Lag = firstCarriedAt.Sub(tIngest)
	return verdict
}
