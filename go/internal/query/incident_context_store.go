// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const incidentContextProviderPagerDuty = "pagerduty"

// ErrIncidentContextNotFound reports a missing incident anchor.
var ErrIncidentContextNotFound = errors.New("incident context not found")

// IncidentContextAmbiguousError reports multiple active incident anchors.
type IncidentContextAmbiguousError struct {
	ProviderIncidentID string
	Candidates         []IncidentContextIncidentCandidate
}

func (e IncidentContextAmbiguousError) Error() string {
	return fmt.Sprintf("incident %q matched multiple active provider scopes; pass scope_id", e.ProviderIncidentID)
}

type incidentContextQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type incidentContextFactRow struct {
	FactID           string
	ScopeID          string
	GenerationID     string
	SourceConfidence string
	SourceURI        string
	SourceRecordID   string
	ObservedAt       time.Time
	Payload          map[string]any
}

// PostgresIncidentContextStore reads active PagerDuty incident source facts.
type PostgresIncidentContextStore struct {
	DB incidentContextQueryer
}

// NewPostgresIncidentContextStore creates the Postgres incident-context store.
func NewPostgresIncidentContextStore(db incidentContextQueryer) PostgresIncidentContextStore {
	return PostgresIncidentContextStore{DB: db}
}

// ReadIncidentContext returns a bounded incident-context snapshot.
func (s PostgresIncidentContextStore) ReadIncidentContext(
	ctx context.Context,
	filter IncidentContextFilter,
) (IncidentContextSnapshot, error) {
	filter = normalizeIncidentContextFilter(filter)
	if s.DB == nil {
		return IncidentContextSnapshot{}, fmt.Errorf("incident context database is required")
	}
	if filter.ProviderIncidentID == "" {
		return IncidentContextSnapshot{}, fmt.Errorf("provider_incident_id is required")
	}
	if filter.Limit <= 0 || filter.Limit > incidentContextMaxLimit+1 {
		return IncidentContextSnapshot{}, fmt.Errorf("limit must be between 1 and %d", incidentContextMaxLimit)
	}
	if _, err := parseIncidentContextBound(filter.Since); err != nil {
		return IncidentContextSnapshot{}, fmt.Errorf("since must be RFC3339: %w", err)
	}
	if _, err := parseIncidentContextBound(filter.Until); err != nil {
		return IncidentContextSnapshot{}, fmt.Errorf("until must be RFC3339: %w", err)
	}

	incidentRows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentContextIncidentsQuery,
		filter.Provider,
		filter.ProviderIncidentID,
		filter.ScopeID,
		2,
	)
	if err != nil {
		return IncidentContextSnapshot{}, fmt.Errorf("list incident context anchors: %w", err)
	}
	if len(incidentRows) == 0 {
		return IncidentContextSnapshot{}, ErrIncidentContextNotFound
	}
	if len(incidentRows) > 1 {
		return IncidentContextSnapshot{}, IncidentContextAmbiguousError{
			ProviderIncidentID: filter.ProviderIncidentID,
			Candidates:         incidentContextCandidates(incidentRows),
		}
	}

	incident := decodeIncidentContextIncident(incidentRows[0])
	timeline, err := s.readIncidentTimeline(ctx, filter, incidentRows[0])
	if err != nil {
		return IncidentContextSnapshot{}, err
	}
	changes, err := s.readIncidentChangeCandidates(ctx, filter, incident, incidentRows[0])
	if err != nil {
		return IncidentContextSnapshot{}, err
	}
	routingEvidence, err := s.readIncidentRoutingEvidence(ctx, incident)
	if err != nil {
		return IncidentContextSnapshot{}, err
	}
	runtimeEvidence, err := s.readIncidentRuntimeEvidence(ctx, incident)
	if err != nil {
		return IncidentContextSnapshot{}, err
	}
	reviewEvidence, err := s.readIncidentReviewWorkItemEvidence(ctx, incident, changes, runtimeEvidence)
	if err != nil {
		return IncidentContextSnapshot{}, err
	}
	evidencePath := append([]IncidentContextEvidenceEdge(nil), routingEvidence...)
	evidencePath = append(evidencePath, runtimeEvidence...)
	evidencePath = append(evidencePath, reviewEvidence...)

	return IncidentContextSnapshot{
		Query: IncidentContextQuery{
			Provider:           filter.Provider,
			ProviderIncidentID: filter.ProviderIncidentID,
			ScopeID:            filter.ScopeID,
			ServiceID:          firstNonEmpty(filter.ServiceID, incident.Service.ID),
			Since:              filter.Since,
			Until:              filter.Until,
			Limit:              filter.Limit,
		},
		Incident:       incident,
		Timeline:       timeline,
		RelatedChanges: changes,
		EvidencePath:   evidencePath,
	}, nil
}

func (s PostgresIncidentContextStore) readIncidentTimeline(
	ctx context.Context,
	filter IncidentContextFilter,
	incident incidentContextFactRow,
) ([]IncidentContextTimelineEvent, error) {
	rows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentContextTimelineQuery,
		filter.ProviderIncidentID,
		incident.ScopeID,
		incident.GenerationID,
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident timeline: %w", err)
	}
	events := make([]IncidentContextTimelineEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, decodeIncidentContextTimelineEvent(row))
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})
	return events, nil
}

func (s PostgresIncidentContextStore) readIncidentChangeCandidates(
	ctx context.Context,
	filter IncidentContextFilter,
	incident IncidentContextIncident,
	incidentRow incidentContextFactRow,
) ([]IncidentContextChangeCandidate, error) {
	serviceID := firstNonEmpty(filter.ServiceID, incident.Service.ID)
	if serviceID == "" {
		return nil, nil
	}
	since, until := incidentChangeWindow(filter, incident)
	rows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentContextChangeCandidatesQuery,
		serviceID,
		incidentRow.ScopeID,
		incidentRow.GenerationID,
		incidentContextSQLTime(since),
		incidentContextSQLTime(until),
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident change candidates: %w", err)
	}
	changes := make([]IncidentContextChangeCandidate, 0, len(rows))
	for _, row := range rows {
		change := decodeIncidentContextChangeCandidate(row)
		if incidentChangeInWindow(change, since, until) {
			changes = append(changes, change)
		}
	}
	sort.SliceStable(changes, func(i, j int) bool {
		return changes[i].Timestamp < changes[j].Timestamp
	})
	return changes, nil
}

func (s PostgresIncidentContextStore) queryIncidentContextRows(
	ctx context.Context,
	query string,
	args ...any,
) ([]incidentContextFactRow, error) {
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []incidentContextFactRow
	for rows.Next() {
		var row incidentContextFactRow
		var payloadBytes []byte
		if err := rows.Scan(
			&row.FactID,
			&row.ScopeID,
			&row.GenerationID,
			&row.SourceConfidence,
			&row.SourceURI,
			&row.SourceRecordID,
			&row.ObservedAt,
			&payloadBytes,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payloadBytes, &row.Payload); err != nil {
			return nil, fmt.Errorf("decode incident context fact: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func normalizeIncidentContextFilter(filter IncidentContextFilter) IncidentContextFilter {
	filter.Provider = strings.ToLower(strings.TrimSpace(filter.Provider))
	if filter.Provider == "" {
		filter.Provider = incidentContextProviderPagerDuty
	}
	filter.ProviderIncidentID = strings.TrimSpace(filter.ProviderIncidentID)
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.ServiceID = strings.TrimSpace(filter.ServiceID)
	filter.Since = strings.TrimSpace(filter.Since)
	filter.Until = strings.TrimSpace(filter.Until)
	return filter
}

func parseIncidentContextBound(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

func incidentContextSQLTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func incidentChangeWindow(
	filter IncidentContextFilter,
	incident IncidentContextIncident,
) (time.Time, time.Time) {
	since, _ := parseIncidentContextBound(filter.Since)
	until, _ := parseIncidentContextBound(filter.Until)
	if !since.IsZero() || !until.IsZero() {
		return since, until
	}
	createdAt, _ := time.Parse(time.RFC3339, incident.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, firstNonEmpty(incident.ResolvedAt, incident.UpdatedAt))
	if !createdAt.IsZero() {
		since = createdAt.Add(-1 * time.Hour)
	}
	if !updatedAt.IsZero() {
		until = updatedAt.Add(1 * time.Hour)
	}
	return since, until
}

func incidentChangeInWindow(
	change IncidentContextChangeCandidate,
	since time.Time,
	until time.Time,
) bool {
	if since.IsZero() && until.IsZero() {
		return true
	}
	timestamp, err := time.Parse(time.RFC3339, change.Timestamp)
	if err != nil {
		return false
	}
	if !since.IsZero() && timestamp.Before(since) {
		return false
	}
	if !until.IsZero() && timestamp.After(until) {
		return false
	}
	return true
}
