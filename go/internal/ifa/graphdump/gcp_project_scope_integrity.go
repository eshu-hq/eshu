// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// GCPProjectScopeExpectation defines one project-scoped fixture partition.
type GCPProjectScopeExpectation struct {
	ProjectID string
	EdgeCount int
}

// ValidateGCPProjectEdgeScopes verifies the exact project-scoped GCP edge set
// expected by a caller-owned fixture. It deliberately rejects unexpected GCP
// scopes instead of claiming that organization, folder, or custom collector
// scopes obey project-local endpoint ownership.
func ValidateGCPProjectEdgeScopes(
	ctx context.Context,
	reader Reader,
	expected map[string]GCPProjectScopeExpectation,
) (int, error) {
	if len(expected) == 0 {
		return 0, fmt.Errorf("validate GCP project edge scopes: expected scope map is empty")
	}
	for scopeID, expectation := range expected {
		projectID, ok := gcpProjectFromScope(scopeID)
		if !ok || projectID != expectation.ProjectID || expectation.EdgeCount < 1 {
			return 0, fmt.Errorf("validate GCP project edge scopes: invalid expectation for scope %q", scopeID)
		}
	}

	checked := 0
	counts := make(map[string]int, len(expected))
	err := reader.StreamEdges(ctx, func(edge Edge) error {
		if !strings.HasPrefix(edge.Type, "GCP_") {
			return nil
		}

		scopeID, ok := nonEmptyStringProperty(edge.Props, "scope_id")
		if !ok {
			return fmt.Errorf("GCP edge %q has no non-empty scope_id", edge.Type)
		}
		expectation, ok := expected[scopeID]
		if !ok {
			return fmt.Errorf("GCP edge %q has unexpected fixture scope_id %q", edge.Type, scopeID)
		}
		checked++
		counts[scopeID]++
		evidenceSource, ok := nonEmptyStringProperty(edge.Props, "evidence_source")
		if !ok || evidenceSource != "reducer/gcp-relationships" {
			return fmt.Errorf("GCP edge %q evidence_source %q is not reducer/gcp-relationships", edge.Type, evidenceSource)
		}
		if !containsLabel(edge.FromLabels, "CloudResource") {
			return fmt.Errorf("GCP edge %q source labels %q do not include CloudResource", edge.Type, edge.FromLabels)
		}
		if !containsLabel(edge.ToLabels, "CloudResource") {
			return fmt.Errorf("GCP edge %q target labels %q do not include CloudResource", edge.Type, edge.ToLabels)
		}

		sourceAccount, ok := nonEmptyStringProperty(edge.FromProps, "account_id")
		if !ok || sourceAccount != expectation.ProjectID {
			return fmt.Errorf("GCP edge %q source account_id %q does not match scope project %q", edge.Type, sourceAccount, expectation.ProjectID)
		}
		targetAccount, ok := nonEmptyStringProperty(edge.ToProps, "account_id")
		if !ok || targetAccount != expectation.ProjectID {
			return fmt.Errorf("GCP edge %q target account_id %q does not match scope project %q", edge.Type, targetAccount, expectation.ProjectID)
		}
		return nil
	})
	if err != nil {
		return checked, fmt.Errorf("validate GCP project edge scopes: %w", err)
	}

	scopes := make([]string, 0, len(expected))
	for scopeID := range expected {
		scopes = append(scopes, scopeID)
	}
	sort.Strings(scopes)
	for _, scopeID := range scopes {
		if counts[scopeID] != expected[scopeID].EdgeCount {
			return checked, fmt.Errorf(
				"validate GCP project edge scopes: scope %q edge count = %d, want %d",
				scopeID,
				counts[scopeID],
				expected[scopeID].EdgeCount,
			)
		}
	}
	return checked, nil
}

func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func nonEmptyStringProperty(properties map[string]any, key string) (string, bool) {
	value, ok := properties[key].(string)
	return value, ok && value != ""
}

func gcpProjectFromScope(scopeID string) (string, bool) {
	parts := strings.Split(scopeID, ":")
	if len(parts) < 3 || parts[0] != "gcp" || parts[1] != "project" || parts[2] == "" {
		return "", false
	}
	return parts[2], true
}
