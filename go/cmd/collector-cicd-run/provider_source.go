// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// providerRoutedSource dispatches one claim to the underlying claim-aware
// source registered for its scope_id, so multiple CI/CD providers
// (github_actions via ghactionsruntime, gitlab_ci via gitlabciruntime) can
// serve claims for the SAME ci_cd_run collector instance and claim queue.
//
// Routing is by scope_id, decided once at config-parse time
// (parseCICDRunRuntimeConfiguration splits targets by their declared
// provider) — this is a DIFFERENT dispatch axis than
// collector.MultiSourceCollectorHost, which resolves by (collector kind,
// collector instance id) across DIFFERENT collector families sharing one
// fair claim dispatcher. This collector has exactly one (kind, instance)
// whose OWN configured targets span multiple providers, so the routing must
// happen one level down, inside the single Source this instance's
// ClaimedService uses.
type providerRoutedSource struct {
	byScopeID map[string]collector.ClaimedSource
}

// newProviderRoutedSource builds a providerRoutedSource from a scope_id ->
// source map. At least one entry is required: a ci_cd_run collector instance
// with zero configured targets across every provider is a configuration
// error the caller should have already rejected before reaching here.
func newProviderRoutedSource(byScopeID map[string]collector.ClaimedSource) (providerRoutedSource, error) {
	if len(byScopeID) == 0 {
		return providerRoutedSource{}, fmt.Errorf("at least one provider-routed ci/cd run target is required")
	}
	return providerRoutedSource{byScopeID: byScopeID}, nil
}

// NextClaimed implements collector.ClaimedSource by resolving the dispatched
// claim's scope_id to its registered provider source and delegating.
func (s providerRoutedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	source, ok := s.byScopeID[item.ScopeID]
	if !ok {
		return collector.CollectedGeneration{}, false, fmt.Errorf(
			"ci/cd run scope %q has no registered provider source", item.ScopeID,
		)
	}
	return source.NextClaimed(ctx, item)
}
