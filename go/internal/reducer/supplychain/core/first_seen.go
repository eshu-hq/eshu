// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// This file owns the durable finding first-seen stamp for eshu-hq/eshu#11031
// criterion 6 (per-source time from upstream publish to finding), under the
// arbiter fork ruling: EPSS/KEV are excluded from criterion 6, the publish
// instants are the advisory-publishing sources' own clocks (OSV
// record.Published, NVD published, GHSA advisory.PublishedAt, CVE List
// CVEMetadata.DatePublished), and the measured lag is first-seen minus
// publish.
//
// Contract classification: additive-optional minor. The stamp and its
// publish-source credit ride the payload's schemaless provenance map beside
// the existing selected_* keys (never the typed SDK struct, which no code in
// this repository may change: no collector change, no factschema revision).
// Payload bytes therefore decode on older readers, which ignore unknown
// provenance keys, and the stamp never enters the content or identity
// digests, so a stamp-carrying rewrite compares identical to its stored row
// instead of churning the changed set forever. The cohort classifier is pure
// and has no wire surface at all.
//
// Durability and set-once semantics: the stamp table
// (supply_chain_impact_finding_first_seen, migration 357) holds one row per
// stable fact key across all scopes and generations. The claim
// (INSERT ... ON CONFLICT DO NOTHING, then SELECT) runs inside the write
// transaction after fencing admission, so a rejected stale pass never
// stamps, and the unique constraint makes concurrent first claims of one key
// idempotent: exactly one row survives, every claimant resolves to it.
// Retraction never deletes the row, so a restored-after-retraction finding
// keeps its original set-once stamp. Advisory-group merge/split never moves
// the stable key (it carries no group members), so every member finding
// keeps its own stamp with no earliest-member election needed.
// The cassette admission cell (go/cmd/ifa/enqueue_advisory_groups.go) replays
// committed payload bytes verbatim: stamped rows keep their stamp through the
// replay via the offered instant below, unstamped legacy rows stay unstamped
// and excluded (counted).
//
// Provenance key names follow the existing selected_* convention so the
// query surface keeps reading only the keys it knows.

// First-seen provenance keys carried on the finding payload beside the
// existing selected_* provenance.
const (
	// firstSeenProvenanceKey is the RFC3339Nano UTC instant of the earliest
	// durable reducer write of the finding's stable key.
	firstSeenProvenanceKey = "first_seen_at"
	// publishSourceProvenanceKey names the collector source whose
	// observation supplied AdvisoryPublishedAt (osv, nvd, ghsa, cve_list,
	// ...): the cohort the lag sample belongs to.
	publishSourceProvenanceKey = "selected_publish_source"
)

// FirstSeenStamp is the resolved durable stamp for one stable fact key: the
// earliest write instant and the publish-source credit beside it.
type FirstSeenStamp struct {
	FirstSeenAt   time.Time
	PublishSource string
}

// SupplyChainImpactFirstSeenClaim offers one stable key for stamping.
// OfferedAt carries a pre-stamped payload's instant (cassette replay on a
// fresh database): a valid offer (non-zero, not after now) is stored as the
// stamp instead of now, so a replay keeps its original first-seen. The
// normal build path offers nothing and stamps now on first write.
type SupplyChainImpactFirstSeenClaim struct {
	StableKey     string
	PublishSource string
	OfferedAt     time.Time
}

// SupplyChainImpactFirstSeenStore claims durable first-seen stamps. The
// production write transaction implements it; a transaction without it (all
// current test fakes) writes findings without stamps, which the cohort
// classifier then excludes with the no-stamp reason.
type SupplyChainImpactFirstSeenStore interface {
	ClaimSupplyChainImpactFirstSeen(ctx context.Context, claims []SupplyChainImpactFirstSeenClaim, now time.Time) (map[string]FirstSeenStamp, error)
}

// ResolveFirstSeenOffer returns the instant a new stamp row stores: a valid
// offered instant (a replayed payload's own first-seen, never in the future)
// wins over now so replays keep their original stamp; anything else stamps
// the write time. Pure, so the clamp is unit-provable without a database.
func ResolveFirstSeenOffer(offered, now time.Time) time.Time {
	if !offered.IsZero() && !offered.After(now) {
		return offered.UTC()
	}
	return now.UTC()
}

// publishSourcesForWrite carries each finding's publish-source credit (the
// source of the provenance-selected observation that supplied
// AdvisoryPublishedAt) alongside its row, by index.
func publishSourcesForWrite(findings []SupplyChainImpactFinding) []string {
	sources := make([]string, len(findings))
	for i, finding := range findings {
		sources[i] = finding.AdvisoryPublishSource
	}
	return sources
}

// FirstSeenClaimsForRows builds one claim per row, keyed on the row's stable
// fact key. Sources override by index (the commit path credits the finding
// that built the row); a nil entry falls back to the payload's own
// selected_publish_source, and rows the normal build just made carry no
// payload yet and offer nothing. The offered instant rides a pre-stamped
// payload when the row already carries one (cassette admission): the raw
// offer passes through and each store resolves it against now with
// ResolveFirstSeenOffer, so a future offer clamps to the write time.
func FirstSeenClaimsForRows(rows []factwrite.VersionedRow, sources []string) []SupplyChainImpactFirstSeenClaim {
	claims := make([]SupplyChainImpactFirstSeenClaim, 0, len(rows))
	for i := range rows {
		if rows[i].StableFactKey == "" {
			continue
		}
		claim := SupplyChainImpactFirstSeenClaim{StableKey: rows[i].StableFactKey}
		if i < len(sources) && sources[i] != "" {
			claim.PublishSource = sources[i]
		} else if source := parseFirstSeenPayloadSource(rows[i].Payload); source != "" {
			claim.PublishSource = source
		}
		if offered, ok := parseFirstSeenPayloadOffer(rows[i].Payload); ok {
			claim.OfferedAt = offered
		}
		claims = append(claims, claim)
	}
	return claims
}

// parseFirstSeenPayloadSource reads a payload's own publish-source credit,
// for rows that carry their provenance already (cassette admission).
func parseFirstSeenPayloadSource(payloadJSON string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return ""
	}
	provenance, ok := payload["provenance"].(map[string]any)
	if !ok {
		return ""
	}
	source, _ := provenance[publishSourceProvenanceKey].(string)
	return strings.TrimSpace(source)
}

// parseFirstSeenPayloadOffer reads a pre-stamped payload's first_seen_at, if
// the row already carries a well-formed one (cassette replay). It reports
// false for absent or malformed offers; the store then resolves the claim to
// now (or clamps a future offer to now) through ResolveFirstSeenOffer.
func parseFirstSeenPayloadOffer(payloadJSON string) (time.Time, bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return time.Time{}, false
	}
	provenance, ok := payload["provenance"].(map[string]any)
	if !ok {
		return time.Time{}, false
	}
	raw, ok := provenance[firstSeenProvenanceKey].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	offered, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false
	}
	return offered.UTC(), true
}

// marshalStampedPayloads marshals each encoded payload map to its row with
// the resolved stamp block set on its provenance. It is the only marshal the
// writer does per row, so the stamp costs two map inserts, not a remarshal
// loop. Both stamp keys are set together, never one without the other.
// Rows whose key resolved no stamp (no store on the transaction, which unit
// tests exercise) marshal stamp-free, byte-identical to the unstamped build.
// Digests were computed from the typed struct before the stamp keys existed,
// so neither digest covers the stamp.
func marshalStampedPayloads(rows []factwrite.VersionedRow, payloads []map[string]any, stamps map[string]FirstSeenStamp) error {
	if len(payloads) != len(rows) {
		return fmt.Errorf("stamp %d payloads onto %d rows: length mismatch", len(payloads), len(rows))
	}
	for i := range rows {
		stamp, ok := stamps[rows[i].StableFactKey]
		if ok && !stamp.FirstSeenAt.IsZero() {
			provenance, ok := payloads[i]["provenance"].(map[string]any)
			if !ok {
				provenance = map[string]any{}
			}
			provenance[firstSeenProvenanceKey] = stamp.FirstSeenAt.UTC().Format(time.RFC3339Nano)
			provenance[publishSourceProvenanceKey] = stamp.PublishSource
			payloads[i]["provenance"] = provenance
		}
		encoded, err := json.Marshal(payloads[i])
		if err != nil {
			return fmt.Errorf("marshal stamped finding payload %q: %w", rows[i].FactID, err)
		}
		rows[i].Payload = string(encoded)
	}
	return nil
}

// StampPrebuiltFindingPayloads patches prebuilt rows' payload JSON with
// their resolved stamps. Only the cassette admission cell uses it: its rows
// arrive with payload bytes already encoded, so unlike the writer (which
// marshals maps once, above) the cell must unmarshal, stamp, and remarshal.
// Cassette admissions are rare and small beside reducer passes, and a row
// whose key resolved no stamp keeps its payload byte-identical.
func StampPrebuiltFindingPayloads(rows []factwrite.VersionedRow, stamps map[string]FirstSeenStamp) error {
	for i := range rows {
		stamp, ok := stamps[rows[i].StableFactKey]
		if !ok || stamp.FirstSeenAt.IsZero() || rows[i].Payload == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(rows[i].Payload), &payload); err != nil {
			return fmt.Errorf("decode prebuilt finding payload %q: %w", rows[i].FactID, err)
		}
		provenance, ok := payload["provenance"].(map[string]any)
		if !ok {
			provenance = map[string]any{}
		}
		provenance[firstSeenProvenanceKey] = stamp.FirstSeenAt.UTC().Format(time.RFC3339Nano)
		provenance[publishSourceProvenanceKey] = stamp.PublishSource
		payload["provenance"] = provenance
		patched, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode stamped prebuilt finding payload %q: %w", rows[i].FactID, err)
		}
		rows[i].Payload = string(patched)
	}
	return nil
}

// ParsePublishInstant parses an advisory publish instant with the same
// parser the newest-copy election uses (RFC3339, then the unzoned NVD
// layout), so the criterion lag and the copy election agree on what an
// instant means. It reports false for empty or unparseable input, which the
// classifier counts as an exclusion rather than zero-valuing into a lag.
func ParsePublishInstant(raw string) (time.Time, bool) {
	return parseAdvisoryInstant(raw)
}

// Publish cohort exclusion reasons. Each names a counted QA exclusion so a
// thin cohort cannot masquerade as a budget.
const (
	// PublishCohortExclusionBackfill marks a sample published before T0: it
	// measures fleet age, not publish-to-finding latency.
	PublishCohortExclusionBackfill = "backfill-before-t0"
	// PublishCohortExclusionNoStamp marks a finding with no durable stamp
	// (written before stamp deployment): first-seen is never back-estimated.
	PublishCohortExclusionNoStamp = "no-stamp"
	// PublishCohortExclusionUnparseablePublish marks an empty or
	// unparseable published_at (real for converted OSV records).
	PublishCohortExclusionUnparseablePublish = "unparseable-publish"
	// PublishCohortExclusionPublishAfterFirstSeen marks a publish instant
	// after first-seen (clock skew, future-dated correction), which would
	// otherwise poison budget stats with a negative lag.
	PublishCohortExclusionPublishAfterFirstSeen = "publish-after-first-seen"
)

// PublishCohortVerdict is the steady-state cohort decision for one finding:
// either an in-cohort sample with its lag (first-seen minus publish), or an
// exclusion with its counted reason.
type PublishCohortVerdict struct {
	InCohort        bool
	ExclusionReason string
	Lag             time.Duration
	PublishInstant  time.Time
}

// ClassifyPublishCohortSample decides whether one finding enters the
// steady-state publish-to-finding cohort. A sample enters only if
// published_at parses, firstSeen is real, published_at is not after
// firstSeen, and published_at >= t0 (per-source T0: the later of stamp
// deployment and that source's landing/freshness precondition; #9432 gates
// the osv T0). Pure: the QA harness supplies T0, this function only judges.
func ClassifyPublishCohortSample(publishedAt string, firstSeen time.Time, t0 time.Time) PublishCohortVerdict {
	if firstSeen.IsZero() {
		return PublishCohortVerdict{ExclusionReason: PublishCohortExclusionNoStamp}
	}
	published, ok := ParsePublishInstant(publishedAt)
	if !ok {
		return PublishCohortVerdict{ExclusionReason: PublishCohortExclusionUnparseablePublish}
	}
	if published.After(firstSeen) {
		return PublishCohortVerdict{ExclusionReason: PublishCohortExclusionPublishAfterFirstSeen}
	}
	if published.Before(t0) {
		return PublishCohortVerdict{ExclusionReason: PublishCohortExclusionBackfill}
	}
	return PublishCohortVerdict{InCohort: true, Lag: firstSeen.Sub(published), PublishInstant: published}
}
