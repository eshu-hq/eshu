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
	SchemaVersion    string
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

	incident, ok := decodeIncidentContextIncident(incidentRows[0])
	if !ok {
		// The sole anchor row matched the query but failed typed decode (no
		// usable provider_incident_id or source_record_id identity, or an
		// unsupported schema major): there is no well-formed incident to
		// answer for, so this is indistinguishable from no match at all.
		return IncidentContextSnapshot{}, ErrIncidentContextNotFound
	}
	timeline, timelineTruncated, err := s.readIncidentTimeline(ctx, filter, incidentRows[0])
	if err != nil {
		return IncidentContextSnapshot{}, err
	}
	changes, changesTruncated, err := s.readIncidentChangeCandidates(ctx, filter, incident, incidentRows[0])
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
	reviewEvidence, err := s.readIncidentReviewWorkItemEvidence(ctx, runtimeEvidence)
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
		// Truncated is the fetched-count truth from the timeline and
		// related-change reads (each derives it from its raw fetched row count,
		// not the decoded count), so a row dropped by typed decode inside the
		// visible window cannot make a truncated page look complete (#4733
		// sibling). The handler no longer re-derives Truncated from len().
		Truncated: timelineTruncated || changesTruncated,
	}, nil
}

// readIncidentTimeline returns the visible timeline window plus a truncation
// flag derived from the RAW fetched row count, never from the decoded count
// (#4733 sibling). filter.Limit is the "+1" lookahead fetch bound the handler
// passes (requested limit + 1); the visible window is the first filter.Limit-1
// fetched rows, decoded (dropping typed-decode failures within it). truncated
// is true when the lookahead row was fetched, so a row that fails decode inside
// the window can never make a truncated timeline report itself complete.
func (s PostgresIncidentContextStore) readIncidentTimeline(
	ctx context.Context,
	filter IncidentContextFilter,
	incident incidentContextFactRow,
) ([]IncidentContextTimelineEvent, bool, error) {
	rows, err := s.queryIncidentContextRows(
		ctx,
		listIncidentContextTimelineQuery,
		filter.ProviderIncidentID,
		incident.ScopeID,
		incident.GenerationID,
		filter.Limit,
	)
	if err != nil {
		return nil, false, fmt.Errorf("list incident timeline: %w", err)
	}
	window, truncated := incidentContextVisibleWindow(rows, filter.Limit)
	events := make([]IncidentContextTimelineEvent, 0, len(window))
	for _, row := range window {
		event, ok := decodeIncidentContextTimelineEvent(row)
		if !ok {
			continue
		}
		events = append(events, event)
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})
	return events, truncated, nil
}

// incidentContextVisibleWindow splits a fetched row slice into the visible
// window and a truncation flag, mirroring buildWorkItemEvidencePage's #4733
// fix for the incident-context reads. fetchLimit is the SQL "+1" lookahead
// bound; the visible window is the first fetchLimit-1 rows in fetch order, and
// truncated is true when more than that were fetched (the lookahead row is
// present). Both truncation and the window come from the RAW fetched count, so
// a later typed-decode drop inside the window cannot corrupt either.
func incidentContextVisibleWindow(rows []incidentContextFactRow, fetchLimit int) ([]incidentContextFactRow, bool) {
	visibleLimit := fetchLimit - 1
	if visibleLimit < 0 {
		visibleLimit = 0
	}
	if len(rows) > visibleLimit {
		return rows[:visibleLimit], true
	}
	return rows, false
}

// readIncidentChangeCandidates returns the visible related-change window plus a
// truncation flag derived from the RAW fetched row count, matching
// readIncidentTimeline and the #4733 work-item fix. The visible window is the
// first filter.Limit-1 fetched rows; a row inside it that fails typed decode
// (or falls outside the change time window) is dropped, but truncated still
// reflects whether the lookahead row was fetched.
func (s PostgresIncidentContextStore) readIncidentChangeCandidates(
	ctx context.Context,
	filter IncidentContextFilter,
	incident IncidentContextIncident,
	incidentRow incidentContextFactRow,
) ([]IncidentContextChangeCandidate, bool, error) {
	serviceID := firstNonEmpty(filter.ServiceID, incident.Service.ID)
	if serviceID == "" {
		return nil, false, nil
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
		return nil, false, fmt.Errorf("list incident change candidates: %w", err)
	}
	window, truncated := incidentContextVisibleWindow(rows, filter.Limit)
	changes := make([]IncidentContextChangeCandidate, 0, len(window))
	for _, row := range window {
		change, ok := decodeIncidentContextChangeCandidate(row)
		if !ok {
			continue
		}
		if incidentChangeInWindow(change, since, until) {
			changes = append(changes, change)
		}
	}
	sort.SliceStable(changes, func(i, j int) bool {
		return changes[i].Timestamp < changes[j].Timestamp
	})
	return changes, truncated, nil
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
			&row.SchemaVersion,
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
