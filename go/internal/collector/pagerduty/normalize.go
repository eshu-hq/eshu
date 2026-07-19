// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"strconv"
	"strings"
	"time"
)

type incidentListResponse struct {
	Incidents []incidentJSON `json:"incidents"`
	// More is PagerDuty classic offset pagination's "another page exists"
	// signal. Total is intentionally not decoded: PagerDuty omits it unless
	// the request opts in with total=true, so More is the only reliable
	// continuation signal (see docs/public/reference/environment-collectors.md).
	More bool `json:"more"`
}

type incidentJSON struct {
	ID               string           `json:"id"`
	IncidentNumber   any              `json:"incident_number"`
	Title            string           `json:"title"`
	Status           string           `json:"status"`
	Urgency          string           `json:"urgency"`
	Priority         referenceJSON    `json:"priority"`
	Service          referenceJSON    `json:"service"`
	EscalationPolicy referenceJSON    `json:"escalation_policy"`
	Teams            []referenceJSON  `json:"teams"`
	Assignments      []assignmentJSON `json:"assignments"`
	CreatedAt        string           `json:"created_at"`
	UpdatedAt        string           `json:"updated_at"`
	ResolvedAt       string           `json:"resolved_at"`
	HTMLURL          string           `json:"html_url"`
}

type assignmentJSON struct {
	Assignee referenceJSON `json:"assignee"`
}

type logEntryListResponse struct {
	LogEntries []logEntryJSON `json:"log_entries"`
	// More is the pagination continuation signal; see incidentListResponse.
	More bool `json:"more"`
}

type logEntryJSON struct {
	ID        string        `json:"id"`
	Type      string        `json:"type"`
	Summary   string        `json:"summary"`
	CreatedAt string        `json:"created_at"`
	Agent     referenceJSON `json:"agent"`
	Channel   channelJSON   `json:"channel"`
	HTMLURL   string        `json:"html_url"`
}

type channelJSON struct {
	Type string `json:"type"`
}

type changeEventListResponse struct {
	ChangeEvents []changeEventJSON `json:"change_events"`
	// More is the pagination continuation signal; see incidentListResponse.
	More bool `json:"more"`
}

type changeEventJSON struct {
	ID        string          `json:"id"`
	Summary   string          `json:"summary"`
	Source    string          `json:"source"`
	Timestamp string          `json:"timestamp"`
	HTMLURL   string          `json:"html_url"`
	Services  []referenceJSON `json:"services"`
	Links     []linkJSON      `json:"links"`
}

type referenceJSON struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	HTMLURL string `json:"html_url"`
}

type linkJSON struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

func normalizeIncidents(input []incidentJSON) []Incident {
	out := make([]Incident, 0, len(input))
	for _, incident := range input {
		out = append(out, Incident{
			ID:             strings.TrimSpace(incident.ID),
			IncidentNumber: int64FromAny(incident.IncidentNumber),
			Title:          strings.TrimSpace(incident.Title),
			Status:         strings.TrimSpace(incident.Status),
			Urgency:        strings.TrimSpace(incident.Urgency),
			Priority:       normalizeReference(incident.Priority),
			Service:        normalizeReference(incident.Service),
			Escalation:     normalizeReference(incident.EscalationPolicy),
			Teams:          normalizeReferences(incident.Teams),
			Assignments:    normalizeAssignments(incident.Assignments),
			CreatedAt:      parseTime(incident.CreatedAt),
			UpdatedAt:      parseTime(incident.UpdatedAt),
			ResolvedAt:     parseTime(incident.ResolvedAt),
			HTMLURL:        safeSourceURI(incident.HTMLURL),
		})
	}
	return out
}

func normalizeLifecycleEvents(incidentID string, input []logEntryJSON) []LifecycleEvent {
	out := make([]LifecycleEvent, 0, len(input))
	for _, event := range input {
		out = append(out, LifecycleEvent{
			ID:         strings.TrimSpace(event.ID),
			IncidentID: strings.TrimSpace(incidentID),
			Type:       strings.TrimSpace(event.Type),
			Actor:      normalizeReference(event.Agent),
			Channel:    strings.TrimSpace(event.Channel.Type),
			Summary:    strings.TrimSpace(event.Summary),
			CreatedAt:  parseTime(event.CreatedAt),
			HTMLURL:    safeSourceURI(event.HTMLURL),
		})
	}
	return out
}

func normalizeChangeEvents(input []changeEventJSON) []ChangeEvent {
	out := make([]ChangeEvent, 0, len(input))
	for _, change := range input {
		out = append(out, ChangeEvent{
			ID:        strings.TrimSpace(change.ID),
			Summary:   strings.TrimSpace(change.Summary),
			Source:    strings.TrimSpace(change.Source),
			Services:  normalizeReferences(change.Services),
			Links:     normalizeLinks(change.Links),
			Timestamp: parseTime(change.Timestamp),
			HTMLURL:   safeSourceURI(change.HTMLURL),
		})
	}
	return out
}

func normalizeReference(ref referenceJSON) Reference {
	return Reference{
		ID:      strings.TrimSpace(ref.ID),
		Type:    strings.TrimSpace(ref.Type),
		Summary: strings.TrimSpace(ref.Summary),
		HTMLURL: safeSourceURI(ref.HTMLURL),
	}
}

func normalizeReferences(refs []referenceJSON) []Reference {
	out := make([]Reference, 0, len(refs))
	for _, ref := range refs {
		normalized := normalizeReference(ref)
		if normalized.ID == "" && normalized.Summary == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func normalizeAssignments(assignments []assignmentJSON) []Reference {
	out := make([]Reference, 0, len(assignments))
	for _, assignment := range assignments {
		normalized := normalizeReference(assignment.Assignee)
		if normalized.ID == "" && normalized.Summary == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func normalizeLinks(links []linkJSON) []Link {
	out := make([]Link, 0, len(links))
	for _, link := range links {
		out = append(out, Link{
			Href: safeSourceURI(link.Href),
			Text: strings.TrimSpace(link.Text),
		})
	}
	return out
}

func parseTime(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}
	}
	value, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Time{}
	}
	return value.UTC()
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}
