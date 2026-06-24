// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"fmt"
	"strings"
)

func validateTerraformStateBackendFilters(
	filters []terraformStateBackendFilterConfig,
	targetScopes map[string]terraformStateTargetScopeConfig,
) (bool, error) {
	usesS3 := false
	for index, filter := range filters {
		backend := strings.ToLower(strings.TrimSpace(filter.BackendKind))
		if backend != filter.BackendKind {
			return false, fmt.Errorf("terraform_state discovery backend_filters %d backend_kind must be lowercase and trimmed", index)
		}
		if backend == "" {
			return false, fmt.Errorf("terraform_state discovery backend_filters %d backend_kind must not be blank", index)
		}
		if backend != "s3" {
			return false, fmt.Errorf("terraform_state discovery backend_filters %d backend_kind %q is unsupported", index, filter.BackendKind)
		}
		usesS3 = true
		bucket := strings.TrimSpace(filter.Bucket)
		if bucket != filter.Bucket {
			return false, fmt.Errorf("terraform_state discovery backend_filters %d bucket must not have surrounding whitespace", index)
		}
		key := strings.Trim(strings.TrimSpace(filter.Key), "/")
		if key != filter.Key {
			return false, fmt.Errorf("terraform_state discovery backend_filters %d key must be relative and trimmed", index)
		}
		region := strings.ToLower(strings.TrimSpace(filter.Region))
		if region != filter.Region {
			return false, fmt.Errorf("terraform_state discovery backend_filters %d region must be lowercase and trimmed", index)
		}
		scope, err := targetScopeForBackendFilter(index, filter, targetScopes)
		if err != nil {
			return false, err
		}
		if scope.TargetScopeID == "" {
			continue
		}
		if len(scope.AllowedBackends) > 0 && !stringSliceContains(scope.AllowedBackends, backend) {
			return false, fmt.Errorf("terraform_state discovery backend_filters %d backend %q is outside allowed_backends for target_scope_id %q", index, backend, scope.TargetScopeID)
		}
		if region != "" && len(scope.AllowedRegions) > 0 && !stringSliceContains(scope.AllowedRegions, region) {
			return false, fmt.Errorf("terraform_state discovery backend_filters %d region %q is outside allowed_regions for target_scope_id %q", index, region, scope.TargetScopeID)
		}
	}
	return usesS3, nil
}

func targetScopeForBackendFilter(
	index int,
	filter terraformStateBackendFilterConfig,
	targetScopes map[string]terraformStateTargetScopeConfig,
) (terraformStateTargetScopeConfig, error) {
	scopeID := strings.TrimSpace(filter.TargetScopeID)
	if scopeID != filter.TargetScopeID {
		return terraformStateTargetScopeConfig{}, fmt.Errorf("terraform_state discovery backend_filters %d target_scope_id must not have surrounding whitespace", index)
	}
	if len(targetScopes) == 0 {
		if scopeID != "" {
			return terraformStateTargetScopeConfig{}, fmt.Errorf("terraform_state discovery backend_filters %d target_scope_id %q is unknown", index, scopeID)
		}
		return terraformStateTargetScopeConfig{}, nil
	}
	if scopeID != "" {
		scope, ok := targetScopes[scopeID]
		if !ok {
			return terraformStateTargetScopeConfig{}, fmt.Errorf("terraform_state discovery backend_filters %d target_scope_id %q is unknown", index, scopeID)
		}
		return scope, nil
	}
	if len(targetScopes) == 1 {
		for _, scope := range targetScopes {
			return scope, nil
		}
	}
	return terraformStateTargetScopeConfig{}, fmt.Errorf("terraform_state discovery backend_filters %d target_scope_id is required when multiple target_scopes are configured", index)
}
