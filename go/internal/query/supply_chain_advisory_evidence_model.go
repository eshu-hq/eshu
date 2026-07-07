// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type advisoryEvidenceAccumulator struct {
	row             AdvisoryEvidenceRow
	cveIDs          map[string]struct{}
	ghsaIDs         map[string]struct{}
	osvIDs          map[string]struct{}
	sourceIDs       map[string]struct{}
	evidenceFactIDs map[string]struct{}
	confidences     map[string]struct{}
	severityValues  map[string]string
	withdrawnValues map[string]string
	fixedValues     map[string]string
	rangeValues     map[string]string
}

func buildAdvisoryEvidenceRows(facts []advisoryEvidenceFactRow) []AdvisoryEvidenceRow {
	groups := map[string]*advisoryEvidenceAccumulator{}
	for _, fact := range facts {
		key := canonicalAdvisoryKey(fact.Payload)
		if key == "" {
			continue
		}
		acc, ok := groups[key]
		if !ok {
			acc = newAdvisoryEvidenceAccumulator(key)
			groups[key] = acc
		}
		acc.addFact(fact)
	}
	out := make([]AdvisoryEvidenceRow, 0, len(groups))
	for _, acc := range groups {
		out = append(out, acc.finish())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AdvisoryKey < out[j].AdvisoryKey
	})
	return out
}

func newAdvisoryEvidenceAccumulator(key string) *advisoryEvidenceAccumulator {
	return &advisoryEvidenceAccumulator{
		row: AdvisoryEvidenceRow{
			AdvisoryKey:     key,
			CanonicalID:     key,
			SourceFreshness: advisoryEvidenceFreshnessCurrent,
		},
		cveIDs:          map[string]struct{}{},
		ghsaIDs:         map[string]struct{}{},
		osvIDs:          map[string]struct{}{},
		sourceIDs:       map[string]struct{}{},
		evidenceFactIDs: map[string]struct{}{},
		confidences:     map[string]struct{}{},
		severityValues:  map[string]string{},
		withdrawnValues: map[string]string{},
		fixedValues:     map[string]string{},
		rangeValues:     map[string]string{},
	}
}

func (a *advisoryEvidenceAccumulator) addFact(fact advisoryEvidenceFactRow) {
	payload := fact.Payload
	source := StringVal(payload, "source")
	advisoryID := StringVal(payload, "advisory_id")
	cveID := StringVal(payload, "cve_id")
	ghsaID := StringVal(payload, "ghsa_id")
	a.addIDs(payload)
	addSet(a.evidenceFactIDs, fact.FactID)
	addSet(a.confidences, fact.SourceConfidence)
	if fact.ObservedAt > a.row.LatestObservedAt {
		a.row.LatestObservedAt = fact.ObservedAt
	}
	if source != "" && advisoryID != "" {
		addSet(a.sourceIDs, source+":"+advisoryID)
	}

	switch fact.FactKind {
	case "vulnerability.cve":
		a.addSourceEvidence(fact, source, advisoryID, cveID, ghsaID)
	case "vulnerability.affected_package":
		a.addAffectedPackage(fact, source, advisoryID, cveID, ghsaID)
	case "vulnerability.affected_product":
		// vulnerability/v1.AffectedProduct (sdk/go/factschema) does not yet
		// declare six of the nine fields this response reads
		// (version_start/end_including/excluding,
		// source_configuration_operator/negate, source_node_operator/negate),
		// so this kind stays on the raw payload path rather than adding a
		// decode wrapper this package could not call losslessly. Flag for a
		// future W1 struct-completion pass.
		a.addAffectedProduct(fact, source, cveID)
	case "vulnerability.epss_score":
		score, err := decodeVulnerabilityEPSSScore(supplyChainFactDecodeInput{FactID: fact.FactID, Payload: payload})
		if err != nil {
			// classified input_invalid (missing cve_id): skip rather than
			// fabricate a zero-valued EPSS observation from an unusable fact.
			return
		}
		a.row.EPSS = append(a.row.EPSS, AdvisoryEPSSObservation{
			Source:      source,
			CVEID:       score.CVEID,
			Probability: workItemDerefString(score.Probability),
			Percentile:  workItemDerefString(score.Percentile),
			ScoreDate:   workItemDerefString(score.ScoreDate),
			FactID:      fact.FactID,
		})
	case "vulnerability.known_exploited":
		kev, err := decodeVulnerabilityKnownExploited(supplyChainFactDecodeInput{FactID: fact.FactID, Payload: payload})
		if err != nil {
			// classified input_invalid (missing cve_id): skip rather than
			// fabricate a zero-valued KEV observation from an unusable fact.
			return
		}
		a.row.KEV = append(a.row.KEV, AdvisoryKEVObservation{
			Source:                     source,
			CVEID:                      kev.CVEID,
			DateAdded:                  workItemDerefString(kev.DateAdded),
			RequiredAction:             workItemDerefString(kev.RequiredAction),
			DueDate:                    workItemDerefString(kev.DueDate),
			KnownRansomwareCampaignUse: workItemDerefString(kev.KnownRansomwareCampaignUse),
			CWEs:                       sortedStrings(kev.CWEs),
			FactID:                     fact.FactID,
		})
	case "vulnerability.reference":
		// vulnerability.reference has no sdk/go/factschema struct yet (not
		// part of the vulnerability/v1 family), so this kind stays on the raw
		// payload path until a future W1 change adds one.
		a.row.References = append(a.row.References, AdvisoryReferenceEvidence{
			Source:        source,
			AdvisoryID:    advisoryID,
			CVEID:         cveID,
			ReferenceType: StringVal(payload, "reference_type"),
			URL:           StringVal(payload, "url"),
			FactID:        fact.FactID,
		})
	}
}

func (a *advisoryEvidenceAccumulator) addIDs(payload map[string]any) {
	for _, value := range []string{
		StringVal(payload, "cve_id"),
		StringVal(payload, "advisory_id"),
		StringVal(payload, "ghsa_id"),
	} {
		a.addID(value)
	}
	for _, value := range StringSliceVal(payload, "aliases") {
		a.addID(value)
	}
	for _, value := range StringSliceVal(payload, "related") {
		a.addID(value)
	}
}

func (a *advisoryEvidenceAccumulator) addID(value string) {
	trimmed := strings.TrimSpace(value)
	switch {
	case isCVEID(trimmed):
		addSet(a.cveIDs, normalizeCVEID(trimmed))
	case isGHSAID(trimmed):
		addSet(a.ghsaIDs, normalizeAdvisoryDisplayID(trimmed))
	case strings.HasPrefix(strings.ToUpper(trimmed), "OSV-"):
		addSet(a.osvIDs, normalizeAdvisoryDisplayID(trimmed))
	}
}

func (a *advisoryEvidenceAccumulator) addSourceEvidence(
	fact advisoryEvidenceFactRow,
	source string,
	advisoryID string,
	cveID string,
	ghsaID string,
) {
	payload := fact.Payload
	cve, err := decodeVulnerabilityCVE(supplyChainFactDecodeInput{FactID: fact.FactID, Payload: payload})
	if err != nil {
		// classified input_invalid (missing advisory_id): skip rather than
		// fabricate a zero-valued source-evidence row from an unusable fact.
		return
	}
	evidence := AdvisorySourceEvidence{
		Source:     source,
		AdvisoryID: advisoryID,
		CVEID:      cveID,
		GHSAID:     ghsaID,
		// TODO(#4795 struct gap): vulnerability/v1.CVE (sdk/go/factschema)
		// has no Aliases field yet; OSV-sourced vulnerability.cve facts carry
		// a real "aliases" list
		// (go/internal/collector/vulnerabilityintelligence/envelope.go). Read
		// raw until a W1 change extends the struct.
		Aliases:       sortedStrings(StringSliceVal(payload, "aliases")),
		PublishedAt:   workItemDerefString(cve.PublishedAt),
		ModifiedAt:    workItemDerefString(cve.ModifiedAt),
		WithdrawnAt:   workItemDerefString(cve.WithdrawnAt),
		SeverityLabel: workItemDerefString(cve.SeverityLabel),
		CVSSScore:     supplyChainDerefFloat64(cve.CVSSScore),
		CVSSVector:    workItemDerefString(cve.CVSSVector),
		// TODO(#4795 struct gap): CVE has no CVSSVectorV2/V3/V4, CVSSMetrics,
		// Severity, or CWEs fields yet; NVD-sourced facts carry cvss_metrics
		// and OSV-sourced facts carry severity. Read raw until a W1 change
		// extends the struct.
		CVSSVectorV2:  StringVal(payload, "cvss_v2"),
		CVSSVectorV3:  StringVal(payload, "cvss_v3"),
		CVSSVectorV4:  StringVal(payload, "cvss_v4"),
		CVSSMetrics:   mapVal(payload, "cvss_metrics"),
		Severity:      stringMapSliceVal(payload, "severity"),
		CWEs:          sortedStrings(StringSliceVal(payload, "cwes")),
		SourceFactIDs: []string{fact.FactID},
	}
	a.row.Sources = append(a.row.Sources, evidence)
	if signature := severitySignature(evidence); signature != "" {
		a.severityValues[source] = signature
	}
	if evidence.WithdrawnAt == "" {
		a.withdrawnValues[source] = "active"
	} else {
		a.withdrawnValues[source] = evidence.WithdrawnAt
	}
}

func (a *advisoryEvidenceAccumulator) addAffectedPackage(
	fact advisoryEvidenceFactRow,
	source string,
	advisoryID string,
	cveID string,
	ghsaID string,
) {
	payload := fact.Payload
	typedPackage, err := decodeVulnerabilityAffectedPackage(supplyChainFactDecodeInput{FactID: fact.FactID, Payload: payload})
	if err != nil {
		// classified input_invalid (missing advisory_id): skip rather than
		// fabricate a zero-valued affected-package row from an unusable fact.
		return
	}
	affected := AdvisoryAffectedPackage{
		Source:        source,
		AdvisoryID:    advisoryID,
		CVEID:         cveID,
		GHSAID:        ghsaID,
		Ecosystem:     workItemDerefString(typedPackage.Ecosystem),
		PackageID:     workItemDerefString(typedPackage.PackageID),
		PURL:          workItemDerefString(typedPackage.PURL),
		AffectedRange: workItemDerefString(typedPackage.AffectedRangeRaw),
		// TODO(#4795 struct gap): vulnerability/v1.AffectedPackage (sdk/go/factschema)
		// has no ParsedAffectedRange field yet (GitLab Gemnasium-sourced facts
		// carry a real "parsed_affected_range" object,
		// go/internal/collector/vulnerabilityintelligence/gitlab_gemnasium_envelope.go),
		// and its typed AffectedRanges field is []AffectedRange, a different Go
		// shape than this response's []map[string]any. Read both raw until a
		// W1 change extends the struct (or a verified round trip is added).
		ParsedAffectedRange: mapVal(payload, "parsed_affected_range"),
		AffectedRanges:      anyMapSliceVal(payload, "affected_ranges"),
		AffectedVersions:    sortedStrings(typedPackage.AffectedVersions),
		FixedVersions:       sortedStrings(typedPackage.FixedVersions),
		SourceFactID:        fact.FactID,
	}
	a.row.AffectedPackages = append(a.row.AffectedPackages, affected)
	if signature := strings.Join(affected.FixedVersions, ","); signature != "" {
		a.fixedValues[source] = signature
	}
	if signature := affectedRangeSignature(affected); signature != "" {
		a.rangeValues[source] = signature
	}
}

func (a *advisoryEvidenceAccumulator) addAffectedProduct(fact advisoryEvidenceFactRow, source string, cveID string) {
	payload := fact.Payload
	a.row.AffectedProducts = append(a.row.AffectedProducts, AdvisoryAffectedProduct{
		Source:                      source,
		CVEID:                       cveID,
		Criteria:                    StringVal(payload, "criteria"),
		MatchCriteriaID:             StringVal(payload, "match_criteria_id"),
		Vulnerable:                  BoolVal(payload, "vulnerable"),
		VersionStartIncluding:       StringVal(payload, "version_start_including"),
		VersionStartExcluding:       StringVal(payload, "version_start_excluding"),
		VersionEndIncluding:         StringVal(payload, "version_end_including"),
		VersionEndExcluding:         StringVal(payload, "version_end_excluding"),
		SourceConfigurationOperator: StringVal(payload, "source_configuration_operator"),
		SourceConfigurationNegate:   BoolVal(payload, "source_configuration_negate"),
		SourceNodeOperator:          StringVal(payload, "source_node_operator"),
		SourceNodeNegate:            BoolVal(payload, "source_node_negate"),
		SourceFactID:                fact.FactID,
	})
}

func (a *advisoryEvidenceAccumulator) finish() AdvisoryEvidenceRow {
	a.row.CVEIDs = setToSortedSlice(a.cveIDs)
	a.row.GHSAIDs = setToSortedSlice(a.ghsaIDs)
	a.row.OSVIDs = setToSortedSlice(a.osvIDs)
	a.row.SourceIDs = setToSortedSlice(a.sourceIDs)
	a.row.EvidenceFactIDs = setToSortedSlice(a.evidenceFactIDs)
	a.row.SourceConfidence = sourceConfidenceLabel(a.confidences)
	sortAdvisoryEvidence(&a.row)
	a.row.SourceDisagreements = []AdvisorySourceDisagreement{
		disagreement("severity", a.severityValues),
		disagreement("withdrawn_status", a.withdrawnValues),
		disagreement("fixed_versions", a.fixedValues),
		disagreement("affected_ranges", a.rangeValues),
	}
	a.row.SourceDisagreements = compactDisagreements(a.row.SourceDisagreements)
	return a.row
}

func canonicalAdvisoryKey(payload map[string]any) string {
	if cve := firstCVEID(payload); cve != "" {
		return cve
	}
	if ghsa := firstGHSAID(payload); ghsa != "" {
		return ghsa
	}
	for _, key := range []string{"advisory_id", "ghsa_id"} {
		if value := normalizeAdvisoryDisplayID(StringVal(payload, key)); value != "" {
			return value
		}
	}
	return ""
}

func firstCVEID(payload map[string]any) string {
	for _, value := range advisoryIdentityCandidates(payload) {
		if isCVEID(value) {
			return normalizeCVEID(value)
		}
	}
	return ""
}

func firstGHSAID(payload map[string]any) string {
	for _, value := range advisoryIdentityCandidates(payload) {
		if isGHSAID(value) {
			return normalizeAdvisoryDisplayID(value)
		}
	}
	return ""
}

func advisoryIdentityCandidates(payload map[string]any) []string {
	values := []string{
		StringVal(payload, "cve_id"),
		StringVal(payload, "ghsa_id"),
		StringVal(payload, "advisory_id"),
	}
	values = append(values, StringSliceVal(payload, "aliases")...)
	values = append(values, StringSliceVal(payload, "correlation_anchors")...)
	return values
}

func isCVEID(value string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(value)), "CVE-")
}

func isGHSAID(value string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(value)), "GHSA-")
}

func normalizeCVEID(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeAdvisoryDisplayID(value string) string {
	trimmed := strings.TrimSpace(value)
	if isCVEID(trimmed) {
		return normalizeCVEID(trimmed)
	}
	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "GHSA-") {
		return "GHSA-" + trimmed[len("GHSA-"):]
	}
	if strings.HasPrefix(upper, "OSV-") {
		return "OSV-" + trimmed[len("OSV-"):]
	}
	return trimmed
}

func severitySignature(value AdvisorySourceEvidence) string {
	switch {
	case value.SeverityLabel != "":
		return strings.TrimSpace(fmt.Sprintf("%s %.1f %s", value.SeverityLabel, value.CVSSScore, value.CVSSVector))
	case value.CVSSVectorV4 != "":
		return value.CVSSVectorV4
	case value.CVSSVectorV3 != "":
		return value.CVSSVectorV3
	case value.CVSSVector != "":
		return value.CVSSVector
	case len(value.Severity) > 0:
		return canonicalJSON(value.Severity)
	default:
		return ""
	}
}

func affectedRangeSignature(value AdvisoryAffectedPackage) string {
	if value.AffectedRange != "" {
		return value.AffectedRange
	}
	if len(value.AffectedRanges) > 0 {
		return canonicalJSON(value.AffectedRanges)
	}
	return ""
}

func disagreement(field string, values map[string]string) AdvisorySourceDisagreement {
	unique := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			unique[value] = struct{}{}
		}
	}
	if len(unique) < 2 {
		return AdvisorySourceDisagreement{}
	}
	out := AdvisorySourceDisagreement{Field: field}
	for source, value := range values {
		if strings.TrimSpace(value) != "" {
			out.Values = append(out.Values, AdvisoryDisagreementValue{Source: source, Value: value})
		}
	}
	sort.Slice(out.Values, func(i, j int) bool {
		if out.Values[i].Source == out.Values[j].Source {
			return out.Values[i].Value < out.Values[j].Value
		}
		return out.Values[i].Source < out.Values[j].Source
	})
	return out
}

func compactDisagreements(values []AdvisorySourceDisagreement) []AdvisorySourceDisagreement {
	out := make([]AdvisorySourceDisagreement, 0, len(values))
	for _, value := range values {
		if value.Field != "" && len(value.Values) > 0 {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalJSON(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(payload)
}
