// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BuildVulnerabilitySuppressions decodes vulnerability.suppression fact
// envelopes into reducer-evaluation form. Envelopes of other fact kinds are
// ignored so callers can pass mixed fact batches without filtering first.
func BuildVulnerabilitySuppressions(envelopes []facts.Envelope) []vulnerabilitySuppression {
	out := make([]vulnerabilitySuppression, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilitySuppressionFactKind {
			continue
		}
		out = append(out, decodeVulnerabilitySuppression(envelope))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].AuthoredAt.Equal(out[j].AuthoredAt) {
			return out[i].AuthoredAt.After(out[j].AuthoredAt)
		}
		return out[i].SuppressionID < out[j].SuppressionID
	})
	return out
}

func decodeVulnerabilitySuppression(envelope facts.Envelope) vulnerabilitySuppression {
	payload := envelope.Payload
	suppressionID := strings.TrimSpace(payloadStr(payload, "suppression_id"))
	if suppressionID == "" {
		suppressionID = envelope.FactID
	}
	scope := decodeVulnerabilitySuppressionScope(payloadMap(payload, "scope"))
	authoredAt, _, _ := parseSuppressionTime(payloadStr(payload, "authored_at"))
	expiresRaw := strings.TrimSpace(payloadStr(payload, "expires_at"))
	expiresAt, expiresPresent, expiresValid := parseSuppressionTime(expiresRaw)
	return vulnerabilitySuppression{
		SuppressionID:        suppressionID,
		Source:               strings.TrimSpace(payloadStr(payload, "source")),
		Justification:        strings.TrimSpace(payloadStr(payload, "justification")),
		Author:               strings.TrimSpace(payloadStr(payload, "author")),
		AuthoredAt:           authoredAt,
		ExpiresAt:            expiresAt,
		ExpiresAtRaw:         expiresRaw,
		ExpiresAtPresent:     expiresPresent,
		ExpiresAtParseFailed: expiresPresent && !expiresValid,
		Reason:               strings.TrimSpace(payloadStr(payload, "reason")),
		Scope:                scope,
		EvidenceRef:          strings.TrimSpace(payloadStr(payload, "evidence_ref")),
		VEXDocumentID:        strings.TrimSpace(payloadStr(payload, "vex_document_id")),
		VEXStatementID:       strings.TrimSpace(payloadStr(payload, "vex_statement_id")),
	}
}

func decodeVulnerabilitySuppressionScope(raw map[string]any) vulnerabilitySuppressionScope {
	return vulnerabilitySuppressionScope{
		CVEID:         strings.TrimSpace(payloadStr(raw, "cve_id")),
		AdvisoryID:    strings.TrimSpace(payloadStr(raw, "advisory_id")),
		PackageID:     strings.TrimSpace(payloadStr(raw, "package_id")),
		PURL:          strings.TrimSpace(payloadStr(raw, "purl")),
		RepositoryID:  strings.TrimSpace(payloadStr(raw, "repository_id")),
		SubjectDigest: strings.TrimSpace(payloadStr(raw, "subject_digest")),
		EvidencePath:  suppressionPayloadStringSlice(raw, "evidence_path"),
	}
}

func suppressionPayloadStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	raw, ok := payload[key]
	if !ok || raw == nil {
		return nil
	}
	var values []string
	switch typed := raw.(type) {
	case []string:
		for _, value := range typed {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				values = append(values, trimmed)
			}
		}
	case []any:
		for _, value := range typed {
			if trimmed := strings.TrimSpace(fmt.Sprint(value)); trimmed != "" && trimmed != "<nil>" {
				values = append(values, trimmed)
			}
		}
	}
	return values
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
