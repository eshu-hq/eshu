// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// Reason codes an advisory retraction carries (#9695). The set is closed.
const (
	// AdvisoryRetractionRejected means every retracting source rejected the
	// record (NVD vulnStatus Rejected, CVE Record Format REJECTED).
	AdvisoryRetractionRejected = "rejected"
	// AdvisoryRetractionWithdrawn means every retracting source withdrew the
	// record (an OSV record with a withdrawn timestamp).
	AdvisoryRetractionWithdrawn = "withdrawn"
	// AdvisoryRetractionRejectedAndWithdrawn means the group mixes both.
	AdvisoryRetractionRejectedAndWithdrawn = "rejected_and_withdrawn"
)

// AdvisoryRetraction records that every source observation of one CVE group
// retracted its advisory. It is the single input to the advisory_retracted
// suppression state, whether the source said rejected or withdrawn.
type AdvisoryRetraction struct {
	// ReasonCode is one of the AdvisoryRetraction* constants.
	ReasonCode string
	// Reason is the sentence a reader sees on the finding.
	Reason string
	// ReplacedBy is the sorted, de-duplicated union of the replacement CVE ids
	// the retracting sources stated. Empty unless a source stated one.
	ReplacedBy []string
	// Sources names each retracting observation, ordered by source then id.
	Sources []AdvisoryRetractionSource
}

// AdvisoryRetractionSource is one source observation that retracted.
type AdvisoryRetractionSource struct {
	Source      string
	AdvisoryID  string
	Code        string
	Status      string
	WithdrawnAt string
	ReplacedBy  []string
}

// advisoryRetractionCode reports whether a source retracted its advisory and
// how. Only two things retract: a status of "rejected" or "withdrawn"
// (case-insensitive, trimmed), or a non-empty OSV withdrawn timestamp. Any other
// status, including one this code has never seen, retracts nothing, so a new
// upstream status can hide no finding (#9695).
func advisoryRetractionCode(status string, withdrawnAt string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "rejected":
		return AdvisoryRetractionRejected
	case "withdrawn":
		return AdvisoryRetractionWithdrawn
	}
	if strings.TrimSpace(withdrawnAt) != "" {
		return AdvisoryRetractionWithdrawn
	}
	return ""
}

// retracted reports whether the observation's source retracted its advisory.
func (o AdvisoryProvenanceObservation) retracted() bool {
	return advisoryRetractionCode(o.Status, o.WithdrawnAt) != ""
}

// cleanReplacementIDs trims, drops blanks, and de-duplicates replacement ids,
// keeping source order.
func cleanReplacementIDs(ids []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// retraction returns the retraction of a CVE group judged on its CVE facts
// alone. The product path uses it: a CPE finding has no affected_package
// sources, so the CVE facts are every contributing source.
func (g supplyChainCVEGroup) retraction(index supplyChainImpactIndex) *AdvisoryRetraction {
	return index.retractionFor(g.cveID, buildAdvisoryProvenanceObservations(g.observations, nil))
}

// retractionFor is retractionFromObservations guarded by the unreadable-record
// rule: a CVE with any vulnerability.cve fact quarantined by a failed decode is
// never retracted, because the unreadable record may be the live source that
// still asserts it. Showing the finding is the safe direction; the quarantine
// is already counted and dead-lettered for the operator.
func (idx supplyChainImpactIndex) retractionFor(cveID string, observations []AdvisoryProvenanceObservation) *AdvisoryRetraction {
	if _, unreadable := idx.unreadableCVEs[cveID]; unreadable {
		return nil
	}
	return retractionFromObservations(observations)
}

// recordUnreadableSupplyChainCVE notes the group key of a quarantined
// vulnerability.cve envelope, read from its raw payload because the typed decode
// failed. It uses supplyChainCVEGroupKey, the key the decode would have given the
// record, with real strings only: a cve_id of the wrong JSON type falls back to
// the advisory id, as the decode's own key would. An envelope with neither
// cannot be tied to a group.
func recordUnreadableSupplyChainCVE(index *supplyChainImpactIndex, envelope facts.Envelope) {
	key := supplyChainCVEGroupKey(
		payloadcore.SemanticPayloadString(envelope.Payload, "cve_id"),
		payloadcore.SemanticPayloadString(envelope.Payload, "advisory_id"),
	)
	if key != "" {
		index.unreadableCVEs[key] = struct{}{}
	}
}

// retractionFromObservations returns the retraction for one finding, or nil
// unless EVERY contributing source observation retracted. The list is the one
// selectAdvisoryProvenance consumes, so a source that supplied an
// affected_package with no usable vulnerability.cve fact (missing, or
// quarantined by a failed decode) is an observation with no status and counts
// as live: a rejection or withdrawal by one source must not hide a
// vulnerability another source still asserts, whatever the state of that
// source's CVE fact. The all-retracted check runs first and allocates nothing,
// because almost every finding is live and this runs once per finding.
func retractionFromObservations(observations []AdvisoryProvenanceObservation) *AdvisoryRetraction {
	if len(observations) == 0 {
		return nil
	}
	var rejected, withdrawn bool
	for _, observation := range observations {
		switch advisoryRetractionCode(observation.Status, observation.WithdrawnAt) {
		case AdvisoryRetractionRejected:
			rejected = true
		case AdvisoryRetractionWithdrawn:
			withdrawn = true
		default:
			return nil
		}
	}
	retraction := &AdvisoryRetraction{
		Sources: make([]AdvisoryRetractionSource, 0, len(observations)),
	}
	seen := make(map[string]struct{}, len(observations))
	var replaced []string
	for _, observation := range observations {
		source := AdvisoryRetractionSource{
			Source:      observation.Source,
			AdvisoryID:  observation.AdvisoryID,
			Code:        advisoryRetractionCode(observation.Status, observation.WithdrawnAt),
			Status:      strings.TrimSpace(observation.Status),
			WithdrawnAt: strings.TrimSpace(observation.WithdrawnAt),
			ReplacedBy:  cleanReplacementIDs(observation.ReplacedBy),
		}
		// Two scopes can each carry the same source record; list it once.
		key := strings.Join([]string{source.Source, source.AdvisoryID, source.Code, source.Status, source.WithdrawnAt, strings.Join(source.ReplacedBy, ",")}, "\x1f")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		replaced = append(replaced, source.ReplacedBy...)
		retraction.Sources = append(retraction.Sources, source)
	}
	sort.Slice(retraction.Sources, func(i, j int) bool {
		a, b := retraction.Sources[i], retraction.Sources[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.AdvisoryID != b.AdvisoryID {
			return a.AdvisoryID < b.AdvisoryID
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Status != b.Status {
			return a.Status < b.Status
		}
		if a.WithdrawnAt != b.WithdrawnAt {
			return a.WithdrawnAt < b.WithdrawnAt
		}
		return strings.Join(a.ReplacedBy, ",") < strings.Join(b.ReplacedBy, ",")
	})
	switch {
	case rejected && withdrawn:
		retraction.ReasonCode = AdvisoryRetractionRejectedAndWithdrawn
	case rejected:
		retraction.ReasonCode = AdvisoryRetractionRejected
	default:
		retraction.ReasonCode = AdvisoryRetractionWithdrawn
	}
	retraction.ReplacedBy = payloadcore.UniqueSortedStrings(replaced)
	retraction.Reason = advisoryRetractionReason(retraction)
	return retraction
}

// advisoryRetractionReason words the retraction for a reader.
func advisoryRetractionReason(retraction *AdvisoryRetraction) string {
	parts := make([]string, 0, len(retraction.Sources))
	for _, source := range retraction.Sources {
		parts = append(parts, source.Source+" "+source.Code+" "+source.AdvisoryID)
	}
	reason := "advisory retracted by its source: " + strings.Join(parts, "; ")
	if len(retraction.ReplacedBy) > 0 {
		reason += "; replaced by " + strings.Join(retraction.ReplacedBy, ", ")
	}
	return reason
}
