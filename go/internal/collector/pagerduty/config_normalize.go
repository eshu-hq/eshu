// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import "strings"

type serviceListResponse struct {
	Services []serviceJSON `json:"services"`
	// More is PagerDuty classic offset pagination's "another page exists"
	// signal; see incidentListResponse in normalize.go.
	More bool `json:"more"`
}

type serviceResponse struct {
	Service serviceJSON `json:"service"`
}

type serviceJSON struct {
	ID               string          `json:"id"`
	Summary          string          `json:"summary"`
	Name             string          `json:"name"`
	Status           string          `json:"status"`
	AlertCreation    string          `json:"alert_creation"`
	EscalationPolicy referenceJSON   `json:"escalation_policy"`
	Teams            []referenceJSON `json:"teams"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
	HTMLURL          string          `json:"html_url"`
	DeletedAt        string          `json:"deleted_at"`
}

type integrationListResponse struct {
	Integrations []integrationJSON `json:"integrations"`
	// More is the pagination continuation signal; see serviceListResponse.
	More bool `json:"more"`
}

type integrationJSON struct {
	ID             string        `json:"id"`
	Summary        string        `json:"summary"`
	Name           string        `json:"name"`
	Type           string        `json:"type"`
	Status         string        `json:"status"`
	Vendor         referenceJSON `json:"vendor"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at"`
	HTMLURL        string        `json:"html_url"`
	IntegrationKey string        `json:"integration_key"`
	RoutingKey     string        `json:"routing_key"`
}

func normalizeConfigServices(input []serviceJSON, matchState string) []ConfigService {
	out := make([]ConfigService, 0, len(input))
	for _, service := range input {
		normalized := normalizeConfigService(service, matchState)
		if normalized.ID == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func normalizeConfigService(input serviceJSON, matchState string) ConfigService {
	status := strings.TrimSpace(input.Status)
	deleted := parseTime(input.DeletedAt)
	return ConfigService{
		ID:            strings.TrimSpace(input.ID),
		Summary:       firstNonBlank(input.Summary, input.Name),
		Status:        status,
		AlertCreation: strings.TrimSpace(input.AlertCreation),
		Escalation:    normalizeReference(input.EscalationPolicy),
		Teams:         normalizeReferences(input.Teams),
		CreatedAt:     parseTime(input.CreatedAt),
		UpdatedAt:     parseTime(input.UpdatedAt),
		HTMLURL:       safeSourceURI(input.HTMLURL),
		Disabled:      status == "disabled",
		Deleted:       !deleted.IsZero() || status == "deleted",
		MatchState:    normalizedConfigMatchState(matchState),
	}
}

func normalizeConfigIntegrations(
	serviceID string,
	input []integrationJSON,
	matchState string,
) ([]ConfigIntegration, int) {
	out := make([]ConfigIntegration, 0, len(input))
	redactions := 0
	for _, integration := range input {
		normalized, redacted := normalizeConfigIntegration(serviceID, integration, matchState)
		redactions += redacted
		if normalized.ID == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out, redactions
}

func normalizeConfigIntegration(serviceID string, input integrationJSON, matchState string) (ConfigIntegration, int) {
	redactions := 0
	if strings.TrimSpace(input.IntegrationKey) != "" {
		redactions++
	}
	if strings.TrimSpace(input.RoutingKey) != "" {
		redactions++
	}
	status := strings.TrimSpace(input.Status)
	return ConfigIntegration{
		ID:                 strings.TrimSpace(input.ID),
		ServiceID:          strings.TrimSpace(serviceID),
		Summary:            firstNonBlank(input.Summary, input.Name),
		Type:               strings.TrimSpace(input.Type),
		VendorID:           strings.TrimSpace(input.Vendor.ID),
		CreatedAt:          parseTime(input.CreatedAt),
		UpdatedAt:          parseTime(input.UpdatedAt),
		HTMLURL:            safeSourceURI(input.HTMLURL),
		Disabled:           status == "disabled",
		Deleted:            status == "deleted",
		MatchState:         normalizedConfigMatchState(matchState),
		RoutingKey:         "",
		RoutingKeyRedacted: redactions > 0,
	}, redactions
}

func normalizedConfigMatchState(_ string) string {
	return ConfigMatchStateNotCompared
}
