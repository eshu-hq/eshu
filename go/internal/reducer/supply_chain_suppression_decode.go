// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	vulnerabilitysuppressionv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerabilitysuppression/v1"
)

// BuildVulnerabilitySuppressions decodes vulnerability.suppression fact
// envelopes into reducer-evaluation form. Envelopes of other fact kinds are
// ignored so callers can pass mixed fact batches without filtering first.
func BuildVulnerabilitySuppressions(
	envelopes []facts.Envelope,
) ([]vulnerabilitySuppression, []quarantinedFact, error) {
	out := make([]vulnerabilitySuppression, 0)
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilitySuppressionFactKind {
			continue
		}
		suppression, err := decodeVulnerabilitySuppression(envelope)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(envelope, err)
			if fatal != nil {
				return nil, quarantined, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		out = append(out, suppression)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].AuthoredAt.Equal(out[j].AuthoredAt) {
			return out[i].AuthoredAt.After(out[j].AuthoredAt)
		}
		return out[i].SuppressionID < out[j].SuppressionID
	})
	return out, quarantined, nil
}

func decodeVulnerabilitySuppression(envelope facts.Envelope) (vulnerabilitySuppression, error) {
	value, err := factschema.DecodeVulnerabilitySuppression(factschemaEnvelope(envelope))
	if err != nil {
		return vulnerabilitySuppression{}, newFactDecodeError(factschema.FactKindVulnerabilitySuppression, err)
	}
	for field, raw := range map[string]string{
		"suppression_id": value.SuppressionID,
		"source":         value.Source,
		"justification":  value.Justification,
		"author":         value.Author,
		"authored_at":    value.AuthoredAt,
	} {
		if strings.TrimSpace(raw) == "" {
			return vulnerabilitySuppression{}, invalidVulnerabilitySuppressionField(field)
		}
	}
	if field := vulnerabilitySuppressionSourceJustificationInvalidField(value.Source, value.Justification); field != "" {
		return vulnerabilitySuppression{}, invalidVulnerabilitySuppressionField(field)
	}
	if vulnerabilitySuppressionTypedScopeEmpty(value.Scope) {
		return vulnerabilitySuppression{}, invalidVulnerabilitySuppressionField("scope")
	}
	authoredAt, authoredPresent, authoredValid := parseSuppressionTime(value.AuthoredAt)
	if !authoredPresent || !authoredValid {
		return vulnerabilitySuppression{}, invalidVulnerabilitySuppressionField("authored_at")
	}
	expiresRaw := optionalSuppressionString(value.ExpiresAt)
	expiresAt, expiresPresent, expiresValid := parseSuppressionTime(expiresRaw)
	return vulnerabilitySuppression{
		SuppressionID:        strings.TrimSpace(value.SuppressionID),
		Source:               strings.TrimSpace(value.Source),
		Justification:        strings.TrimSpace(value.Justification),
		Author:               strings.TrimSpace(value.Author),
		AuthoredAt:           authoredAt,
		ExpiresAt:            expiresAt,
		ExpiresAtRaw:         expiresRaw,
		ExpiresAtPresent:     expiresPresent,
		ExpiresAtParseFailed: expiresPresent && !expiresValid,
		Reason:               optionalSuppressionString(value.Reason),
		Scope:                decodeVulnerabilitySuppressionScope(value.Scope),
		EvidenceRef:          optionalSuppressionString(value.EvidenceRef),
		VEXDocumentID:        optionalSuppressionString(value.VEXDocumentID),
		VEXStatementID:       optionalSuppressionString(value.VEXStatementID),
	}, nil
}

func vulnerabilitySuppressionSourceJustificationInvalidField(source string, justification string) string {
	switch strings.TrimSpace(source) {
	case facts.VulnerabilitySuppressionSourceVEX:
		if strings.TrimSpace(justification) == facts.VulnerabilitySuppressionJustificationNotAffected {
			return ""
		}
	case facts.VulnerabilitySuppressionSourcePolicy:
		switch strings.TrimSpace(justification) {
		case facts.VulnerabilitySuppressionJustificationNotAffected,
			facts.VulnerabilitySuppressionJustificationAcceptedRisk,
			facts.VulnerabilitySuppressionJustificationFalsePositive,
			facts.VulnerabilitySuppressionJustificationIgnored:
			return ""
		}
	case facts.VulnerabilitySuppressionSourceProviderDismissal:
		if strings.TrimSpace(justification) == facts.VulnerabilitySuppressionJustificationProviderDismissed {
			return ""
		}
	default:
		return "source"
	}
	return "justification"
}

func decodeVulnerabilitySuppressionScope(
	value vulnerabilitysuppressionv1.Scope,
) vulnerabilitySuppressionScope {
	return vulnerabilitySuppressionScope{
		CVEID:         optionalSuppressionString(value.CVEID),
		AdvisoryID:    optionalSuppressionString(value.AdvisoryID),
		PackageID:     optionalSuppressionString(value.PackageID),
		PURL:          optionalSuppressionString(value.PURL),
		RepositoryID:  optionalSuppressionString(value.RepositoryID),
		SubjectDigest: optionalSuppressionString(value.SubjectDigest),
		EvidencePath:  cleanSuppressionStrings(value.EvidencePath),
	}
}

func invalidVulnerabilitySuppressionField(field string) error {
	return newFactDecodeError(
		factschema.FactKindVulnerabilitySuppression,
		&factschema.DecodeError{
			FactKind:       factschema.FactKindVulnerabilitySuppression,
			Classification: factschema.ClassificationInputInvalid,
			Field:          field,
		},
	)
}

func optionalSuppressionString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func cleanSuppressionStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func vulnerabilitySuppressionTypedScopeEmpty(value vulnerabilitysuppressionv1.Scope) bool {
	if optionalSuppressionString(value.CVEID) != "" ||
		optionalSuppressionString(value.AdvisoryID) != "" ||
		optionalSuppressionString(value.PackageID) != "" ||
		optionalSuppressionString(value.PURL) != "" ||
		optionalSuppressionString(value.RepositoryID) != "" ||
		optionalSuppressionString(value.SubjectDigest) != "" {
		return false
	}
	return len(cleanSuppressionStrings(value.EvidencePath)) == 0
}

// parseSuppressionTime parses an RFC3339 timestamp from a fact payload and
// reports both whether the value was present and whether it parsed cleanly.
// Callers MUST distinguish "missing" from "invalid": treating an invalid
// timestamp as "no expiration" would silently extend a suppression past its
// intended end.
func parseSuppressionTime(raw string) (parsed time.Time, present, valid bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, true, false
	}
	return t.UTC(), true, true
}
