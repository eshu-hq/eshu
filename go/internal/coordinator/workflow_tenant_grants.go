// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// WorkflowTenantBoundary is the effective hosted boundary for planned claims.
type WorkflowTenantBoundary struct {
	TenantID           string
	WorkspaceID        string
	SubjectClass       string
	PolicyRevisionHash string
}

// WorkflowTenantGrantQuery bounds coordinator grant reads.
type WorkflowTenantGrantQuery struct {
	TenantID     string
	WorkspaceID  string
	SubjectClass string
	ScopeIDs     []string
	AsOf         time.Time
	Limit        int
}

// WorkflowTenantScopeGrant is one active scope grant visible to the coordinator.
type WorkflowTenantScopeGrant struct {
	ScopeID            string
	PolicyRevisionHash string
}

// TenantGrantReader loads active hosted tenant scope grants.
type TenantGrantReader interface {
	ListWorkflowScopeGrants(context.Context, WorkflowTenantGrantQuery) ([]WorkflowTenantScopeGrant, error)
}

func (b WorkflowTenantBoundary) normalize() WorkflowTenantBoundary {
	b.TenantID = strings.TrimSpace(b.TenantID)
	b.WorkspaceID = strings.TrimSpace(b.WorkspaceID)
	b.SubjectClass = strings.TrimSpace(b.SubjectClass)
	b.PolicyRevisionHash = strings.TrimSpace(b.PolicyRevisionHash)
	return b
}

func (b WorkflowTenantBoundary) configured() bool {
	b = b.normalize()
	return b.TenantID != "" || b.WorkspaceID != "" ||
		b.SubjectClass != "" || b.PolicyRevisionHash != ""
}

func (b WorkflowTenantBoundary) validate() error {
	b = b.normalize()
	if !b.configured() {
		return nil
	}
	if b.TenantID == "" || b.WorkspaceID == "" ||
		b.SubjectClass == "" || b.PolicyRevisionHash == "" {
		return fmt.Errorf("workflow tenant boundary requires tenant_id, workspace_id, subject_class, and policy_revision_hash")
	}
	return nil
}

func (s Service) authorizeWorkflowWorkItems(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) ([]workflow.WorkItem, int, error) {
	boundary := s.Config.TenantBoundary.normalize()
	if !boundary.configured() || len(items) == 0 {
		return items, 0, nil
	}
	if err := boundary.validate(); err != nil {
		return nil, 0, err
	}
	if s.TenantGrantReader == nil {
		return nil, 0, fmt.Errorf("tenant grant reader is required when workflow tenant boundary is configured")
	}
	scopeIDs := workflowTenantScopeIDs(items)
	if len(scopeIDs) == 0 {
		return nil, len(items), nil
	}
	asOf := run.CreatedAt
	if asOf.IsZero() {
		asOf = s.now().UTC()
	}
	grants, err := s.TenantGrantReader.ListWorkflowScopeGrants(ctx, WorkflowTenantGrantQuery{
		TenantID:     boundary.TenantID,
		WorkspaceID:  boundary.WorkspaceID,
		SubjectClass: boundary.SubjectClass,
		ScopeIDs:     scopeIDs,
		AsOf:         asOf.UTC(),
		Limit:        len(scopeIDs),
	})
	if err != nil {
		return nil, 0, err
	}
	allowed := make(map[string]string, len(grants))
	for _, grant := range grants {
		scopeID := strings.TrimSpace(grant.ScopeID)
		policyRevision := strings.TrimSpace(grant.PolicyRevisionHash)
		if scopeID == "" || policyRevision == "" {
			continue
		}
		allowed[scopeID] = policyRevision
	}
	filtered := make([]workflow.WorkItem, 0, len(items))
	denied := 0
	for _, item := range items {
		policyRevision, ok := allowed[strings.TrimSpace(item.ScopeID)]
		if !ok || policyRevision != boundary.PolicyRevisionHash {
			denied++
			continue
		}
		item.TenantID = boundary.TenantID
		item.WorkspaceID = boundary.WorkspaceID
		item.SubjectClass = boundary.SubjectClass
		item.PolicyRevisionHash = policyRevision
		filtered = append(filtered, item)
	}
	return filtered, denied, nil
}

func workflowTenantScopeIDs(items []workflow.WorkItem) []string {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		scopeID := strings.TrimSpace(item.ScopeID)
		if scopeID != "" {
			seen[scopeID] = struct{}{}
		}
	}
	scopeIDs := make([]string, 0, len(seen))
	for scopeID := range seen {
		scopeIDs = append(scopeIDs, scopeID)
	}
	sort.Strings(scopeIDs)
	return scopeIDs
}

func filterWorkflowRunRequestedScopeSet(run workflow.Run, items []workflow.WorkItem) workflow.Run {
	if strings.TrimSpace(run.RequestedScopeSet) == "" || len(items) == 0 {
		return run
	}
	allowed := make(map[string]struct{}, len(items))
	for _, item := range items {
		scopeID := strings.TrimSpace(item.ScopeID)
		if scopeID != "" {
			allowed[scopeID] = struct{}{}
		}
	}
	filtered, ok := filterRequestedScopeSetJSON(run.RequestedScopeSet, allowed)
	if !ok {
		run.RequestedScopeSet = "[]"
		return run
	}
	run.RequestedScopeSet = filtered
	return run
}

func filterRequestedScopeSetJSON(raw string, allowed map[string]struct{}) (string, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &object); err == nil {
		targetsRaw, hasTargets := object["targets"]
		if !hasTargets {
			return "[]", true
		}
		filteredTargets, ok := filterRequestedScopeTargets(targetsRaw, allowed)
		if !ok {
			return "", false
		}
		object["targets"] = filteredTargets
		filtered, err := json.Marshal(object)
		if err != nil {
			return "", false
		}
		return string(filtered), true
	}
	filteredTargets, ok := filterRequestedScopeTargets([]byte(raw), allowed)
	if !ok {
		return "", false
	}
	return string(filteredTargets), true
}

func filterRequestedScopeTargets(raw json.RawMessage, allowed map[string]struct{}) (json.RawMessage, bool) {
	var targets []map[string]any
	if err := json.Unmarshal(raw, &targets); err != nil {
		return nil, false
	}
	filtered := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		scopeValue, _ := target["scope_id"].(string)
		if _, ok := allowed[strings.TrimSpace(scopeValue)]; ok {
			filtered = append(filtered, target)
		}
	}
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return nil, false
	}
	return encoded, true
}
