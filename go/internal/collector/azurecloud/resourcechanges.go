// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ResourceChangesPage is one parsed Resource Graph resourcechanges page. It is
// freshness evidence only: rows can explain recent create, update, and delete
// events, but they do not prove final resource state.
type ResourceChangesPage struct {
	// TotalRecords is the provider-reported total matching the query.
	TotalRecords int64
	// Count is the number of change rows in this page.
	Count int64
	// ResultTruncated reports whether the provider truncated the result set.
	ResultTruncated bool
	// SkipToken is the continuation token for the next page; empty means done.
	SkipToken string
	// Changes are the parsed change rows on this page.
	Changes []ResourceChangeRow
}

// ResourceChangeRow is one Resource Graph resourcechanges row. It exposes only
// bounded change metadata and property paths; previous/new property values and
// raw provider bodies are intentionally ignored.
type ResourceChangeRow struct {
	// ID is the provider change record identifier when projected.
	ID string `json:"id"`
	// Properties carries the Resource Graph change properties object.
	Properties ResourceChangeProperties `json:"properties"`
}

// ResourceChangeProperties is the projected Resource Graph change payload.
type ResourceChangeProperties struct {
	// ChangeType is Create, Update, or Delete from Resource Graph.
	ChangeType string `json:"changeType"`
	// TargetResourceID is the ARM identity the change applied to.
	TargetResourceID string `json:"targetResourceId"`
	// TargetResourceType is the provider resource type.
	TargetResourceType string `json:"targetResourceType"`
	// ChangeAttributes carries timestamp, operation, client, and actor metadata.
	ChangeAttributes ResourceChangeAttributes `json:"changeAttributes"`
	// Changes maps changed property paths to details. Only keys/propertyName are
	// retained; before/after values are never copied into facts.
	Changes map[string]ResourcePropertyChange `json:"changes"`
}

// ResourceChangeAttributes carries bounded metadata about who initiated a
// change and when Resource Graph recorded it.
type ResourceChangeAttributes struct {
	// Timestamp is the provider change timestamp.
	Timestamp string `json:"timestamp"`
	// Operation is the Azure RBAC operation/action string.
	Operation string `json:"operation"`
	// ClientType is the bounded client classifier when known.
	ClientType string `json:"clientType"`
	// ChangedBy is the raw actor identity; it is fingerprinted before emission.
	ChangedBy string `json:"changedBy"`
	// ChangedByType is the bounded actor class.
	ChangedByType string `json:"changedByType"`
}

// ResourcePropertyChange carries the changed property path. Value fields are
// omitted by design so parser users cannot accidentally persist them.
type ResourcePropertyChange struct {
	// PropertyName is the changed property path.
	PropertyName string `json:"propertyName"`
}

type resourceChangesRaw struct {
	TotalRecords    int64               `json:"totalRecords"`
	Count           int64               `json:"count"`
	ResultTruncated string              `json:"resultTruncated"`
	SkipToken       string              `json:"$skipToken"`
	Data            []ResourceChangeRow `json:"data"`
}

// ParseResourceChangesPage parses one Resource Graph resourcechanges response
// page. It accepts the table-query shape used by Resource Graph examples and
// keeps only bounded provenance fields: timestamp, change type, target ARM ID,
// operation, client type, actor class/id, and changed property paths.
func ParseResourceChangesPage(raw []byte) (ResourceChangesPage, error) {
	var parsed resourceChangesRaw
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ResourceChangesPage{}, fmt.Errorf("parse resource changes page: %w", err)
	}
	return ResourceChangesPage{
		TotalRecords:    parsed.TotalRecords,
		Count:           parsed.Count,
		ResultTruncated: strings.EqualFold(strings.TrimSpace(parsed.ResultTruncated), "true"),
		SkipToken:       strings.TrimSpace(parsed.SkipToken),
		Changes:         parsed.Data,
	}, nil
}

// TargetARMResourceID returns the raw target ARM identity for the change.
func (r ResourceChangeRow) TargetARMResourceID() string {
	return strings.TrimSpace(r.Properties.TargetResourceID)
}

// ChangeType returns the normalized bounded change type.
func (r ResourceChangeRow) ChangeType() string {
	changeType, err := normalizeChangeType(r.Properties.ChangeType)
	if err != nil {
		return ""
	}
	return changeType
}

// ChangeTime returns the parsed provider change timestamp, or nil when absent
// or invalid so callers can fail closed.
func (r ResourceChangeRow) ChangeTime() *time.Time {
	value := strings.TrimSpace(r.Properties.ChangeAttributes.Timestamp)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

// ChangedPropertyPaths returns sorted, de-duplicated property paths without
// carrying previous or new values.
func (r ResourceChangeRow) ChangedPropertyPaths() []string {
	seen := make(map[string]struct{}, len(r.Properties.Changes))
	for path, change := range r.Properties.Changes {
		if trimmed := strings.TrimSpace(change.PropertyName); trimmed != "" {
			seen[trimmed] = struct{}{}
			continue
		}
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func (r ResourceChangeRow) toObservation(boundary Boundary) (ResourceChangeObservation, error) {
	changeType, err := normalizeChangeType(r.Properties.ChangeType)
	if err != nil {
		return ResourceChangeObservation{}, err
	}
	changeTime := r.ChangeTime()
	if changeTime == nil {
		return ResourceChangeObservation{}, fmt.Errorf("azure resource change row requires timestamp")
	}
	return ResourceChangeObservation{
		Boundary:             boundary,
		TargetARMResourceID:  r.TargetARMResourceID(),
		ChangeType:           changeType,
		ChangeTime:           *changeTime,
		Operation:            r.Properties.ChangeAttributes.Operation,
		ClientType:           r.Properties.ChangeAttributes.ClientType,
		ActorID:              r.Properties.ChangeAttributes.ChangedBy,
		ActorClass:           r.Properties.ChangeAttributes.ChangedByType,
		ChangedPropertyPaths: r.ChangedPropertyPaths(),
		SourceRecordID:       r.ID,
	}, nil
}
