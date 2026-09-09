// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func incidentDeclaredRoutingCandidates(
	incident model.IncidentContextIncident,
	declared []incidentDeclaredPagerDutyRouting,
) []incidentDeclaredPagerDutyRouting {
	serviceName := strings.TrimSpace(incident.Service.Summary)
	out := make([]incidentDeclaredPagerDutyRouting, 0, len(declared))
	for _, item := range declared {
		if serviceName == "" || strings.EqualFold(strings.TrimSpace(item.ServiceName), serviceName) {
			out = append(out, item)
		}
	}
	return out
}

func incidentAppliedRoutingCandidates(
	incident model.IncidentContextIncident,
	applied []incidentAppliedPagerDutyRouting,
) []incidentAppliedPagerDutyRouting {
	serviceID := strings.TrimSpace(incident.Service.ID)
	serviceFingerprint := incidentRoutingShortFingerprint(incident.Service.Summary)
	out := make([]incidentAppliedPagerDutyRouting, 0, len(applied))
	for _, item := range applied {
		if serviceID != "" && strings.EqualFold(strings.TrimSpace(item.ProviderObjectID), serviceID) {
			out = append(out, item)
			continue
		}
		if serviceFingerprint != "" && strings.TrimSpace(item.NameFingerprint) == serviceFingerprint {
			out = append(out, item)
		}
	}
	return out
}

func incidentObservedRoutingCandidates(
	incident model.IncidentContextIncident,
	observed []incidentObservedPagerDutyRouting,
) []incidentObservedPagerDutyRouting {
	serviceID := strings.TrimSpace(incident.Service.ID)
	serviceFingerprint := incidentRoutingConfigFingerprint(incident.Service.Summary)
	out := make([]incidentObservedPagerDutyRouting, 0, len(observed))
	for _, item := range observed {
		if serviceID != "" &&
			(strings.EqualFold(strings.TrimSpace(item.ServiceID), serviceID) ||
				strings.EqualFold(strings.TrimSpace(item.ProviderObjectID), serviceID)) {
			out = append(out, item)
			continue
		}
		if serviceFingerprint != "" && strings.TrimSpace(item.NameFingerprint) == serviceFingerprint {
			out = append(out, item)
		}
	}
	return out
}

func incidentDeclaredRoutingCandidateValues(
	items []incidentDeclaredPagerDutyRouting,
) []model.IncidentContextEvidenceCandidate {
	candidates := make([]model.IncidentContextEvidenceCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, model.IncidentContextEvidenceCandidate{
			ID:     querycontract.FirstNonEmpty(item.EntityID, item.RelativePath),
			Label:  querycontract.FirstNonEmpty(item.EntityName, item.RelativePath),
			Reason: querycontract.FirstNonEmpty(item.Outcome, "declared PagerDuty routing candidate"),
		})
	}
	return candidates
}

func incidentAppliedRoutingCandidateValues(
	items []incidentAppliedPagerDutyRouting,
) []model.IncidentContextEvidenceCandidate {
	candidates := make([]model.IncidentContextEvidenceCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, model.IncidentContextEvidenceCandidate{
			ID:     querycontract.FirstNonEmpty(item.ProviderObjectID, item.TerraformStateAddress, item.FactID),
			Label:  item.TerraformStateAddress,
			Reason: querycontract.FirstNonEmpty(item.Outcome, "applied PagerDuty routing candidate"),
		})
	}
	return candidates
}

func incidentObservedRoutingCandidateValues(
	items []incidentObservedPagerDutyRouting,
) []model.IncidentContextEvidenceCandidate {
	candidates := make([]model.IncidentContextEvidenceCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, model.IncidentContextEvidenceCandidate{
			ID:     querycontract.FirstNonEmpty(item.ServiceID, item.ProviderObjectID, item.FactID),
			Label:  querycontract.FirstNonEmpty(item.Status, item.ServiceID),
			URL:    item.SourceURL,
			Reason: querycontract.FirstNonEmpty(item.Outcome, "live PagerDuty routing candidate"),
		})
	}
	return candidates
}

func incidentRoutingShortFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(value)))
	return hex.EncodeToString(sum[:])[:16]
}

func incidentRoutingConfigFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
