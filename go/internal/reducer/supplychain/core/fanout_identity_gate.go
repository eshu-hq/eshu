// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// The D7 identity gate (#10972, arbiter ruling arbiter-pr4-d7.md (b)). A
// finding row carries two digests. The full content digest
// (contentDigestOf) still decides the row write, the winners dirty capture
// and the advisory-group touch. The identity digest leaves out the fields
// computed only from EPSS and KEV facts, and decides whether a pass
// ORIGINATES a holder fan-out for the row. Without it, every holder whose
// rerun saw a newer EPSS score date re-announced that date to every other
// holder of the CVE: one EPSS refresh fanned out once per copy. The EPSS or
// KEV scope's own pass stays the origin of that change (HOLDER_FANOUT.md,
// "Enrichment refresh"), so a pass whose own generation holds EPSS or KEV
// facts still fans out on the full digest.

// fanOutOutcomeIdentityGated counts, on eshu_dp_supply_chain_impact_fanout_total,
// the lookup ids a non-enrichment pass would have fanned out for on the full
// digest but did not, because only their EPSS or KEV fields changed.
const fanOutOutcomeIdentityGated = "identity_gated"

// identityDigestProvenanceKey is the open provenance key a finding row carries
// its identity digest under, beside content_digest. Both digests leave both
// keys out.
const identityDigestProvenanceKey = "identity_digest"

// enrichmentPayloadKeys are the top-level finding payload fields computed only
// from EPSS and KEV facts, or derived from them together with fields that are
// themselves in the identity digest (reducerderived/v1 findings.go:36-48):
//   - epss_probability, epss_percentile: the selected EPSS observation.
//   - known_exploited: the KEV listing.
//   - priority_reason, priority_score, priority_bucket, priority_reason_codes,
//     priority_contributions, exploited_tier: withSupplyChainImpactPriority
//     (priority.go:91-121). Their other inputs (status, CVSS score and vector,
//     versions, fixed version, reachability, repository, subject digest,
//     deployment ids, missing evidence, retraction, advisory dates) are all
//     identity fields, so a change to them still changes the identity digest.
//
// Every other field is identity. A field added to the payload later is
// identity until it is named here, so it fans out (fail closed).
var enrichmentPayloadKeys = []string{
	"epss_probability",
	"epss_percentile",
	"known_exploited",
	"priority_reason",
	"priority_score",
	"priority_bucket",
	"priority_reason_codes",
	"priority_contributions",
	"exploited_tier",
}

// enrichmentProvenanceKeys are the provenance entries computed only from EPSS
// and KEV facts (provenance.go:420, :423, :462, :465).
var enrichmentProvenanceKeys = map[string]struct{}{
	"selected_epss_score_date":    {},
	"selected_epss_model_version": {},
	"advisory_group_epss_source":  {},
	"advisory_group_kev_source":   {},
}

// isDigestProvenanceKey reports whether a provenance key stores one of the
// row's own digests, which no digest may cover.
func isDigestProvenanceKey(key string) bool {
	return key == contentDigestProvenanceKey || key == identityDigestProvenanceKey
}

// hasDigestProvenanceKey reports whether provenance stores either digest.
func hasDigestProvenanceKey(provenance map[string]any) bool {
	for key := range provenance {
		if isDigestProvenanceKey(key) {
			return true
		}
	}
	return false
}

// identityDigestOf hashes a finding payload without its EPSS and KEV
// enrichment: the full content digest of the payload with
// enrichmentPayloadKeys zeroed and enrichmentProvenanceKeys removed.
func identityDigestOf(payload reducerderivedv1.SupplyChainImpactFinding) string {
	payload.EPSSProbability = ""
	payload.EPSSPercentile = ""
	payload.KnownExploited = false
	payload.PriorityReason = ""
	payload.PriorityScore = 0
	payload.PriorityBucket = ""
	payload.PriorityReasonCodes = nil
	payload.PriorityContributions = nil
	payload.ExploitedTier = nil
	if len(payload.Provenance) > 0 {
		trimmed := make(map[string]any, len(payload.Provenance))
		for key, value := range payload.Provenance {
			if _, enrichment := enrichmentProvenanceKeys[key]; !enrichment {
				trimmed[key] = value
			}
		}
		payload.Provenance = trimmed
	}
	return contentDigestOf(payload)
}

// identityChangedRows returns copies of the pass's rows and the stored rows
// whose digest field holds the identity digest, so the change-set functions
// judge identity change with the same keep, retract and fencing logic they use
// for the full digest. A stored row written before the identity digest existed
// has none and reads as changed (fail closed: it fans out once).
func identityChangedRows(rows []changedRow, stored []storedFinding) ([]changedRow, []storedFinding) {
	idRows := make([]changedRow, len(rows))
	for i, row := range rows {
		row.digest = row.identityDigest
		idRows[i] = row
	}
	idStored := make([]storedFinding, len(stored))
	for i, row := range stored {
		row.digest = row.identityDigest
		idStored[i] = row
	}
	return idRows, idStored
}

// fanOutKindsAreEnrichment reports whether a pass's own scope facts include an
// EPSS or KEV kind (the fanOutCause families epss and kev, alone or mixed):
// such a pass is the documented origin of an enrichment change and fans out
// on the full digest.
func fanOutKindsAreEnrichment(kinds []string) bool {
	for _, kind := range kinds {
		if kind == chain.VulnerabilityEPSSScoreFactKind || kind == chain.VulnerabilityKnownExploitedFactKind {
			return true
		}
	}
	return false
}

// withFanOutOrigin records on the write result whether this pass is an
// enrichment origin, for fanOutOriginIDs.
func withFanOutOrigin(write SupplyChainImpactWriteResult, scopeFactKinds []string) SupplyChainImpactWriteResult {
	write.enrichmentOrigin = fanOutKindsAreEnrichment(scopeFactKinds)
	return write
}

// withFanOutOriginToken records the pass's own evidence-read fencing token
// on the write result, for the fan-out origin stamp (#11613). The handler
// calls it with the token it issued before the evidence load, so the stamp
// orders real passes rather than wall times.
func withFanOutOriginToken(write SupplyChainImpactWriteResult, token int64) SupplyChainImpactWriteResult {
	write.originToken = token
	return write
}

// fanOutOriginIDs returns the lookup ids a pass's own changes fan out for and
// how many ids of the full-digest set the identity gate dropped. An
// enrichment origin, or a writer that did not report identity sets, fans out
// on the full digest. Every other pass fans out only for rows whose identity
// changed. The advisory-group enqueue keeps the full touched set either way.
func fanOutOriginIDs(write SupplyChainImpactWriteResult) ([]string, int) {
	full := unionSortedStrings(write.ChangedCVEIDs, write.TouchedGroupIDs)
	if write.enrichmentOrigin || !write.IdentityReported {
		return full, 0
	}
	gated := unionSortedStrings(write.IdentityChangedCVEIDs, write.IdentityTouchedGroupIDs)
	kept := make(map[string]struct{}, len(gated))
	for _, id := range gated {
		kept[id] = struct{}{}
	}
	dropped := 0
	for _, id := range full {
		if _, ok := kept[id]; !ok {
			dropped++
		}
	}
	return gated, dropped
}
