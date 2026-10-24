// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// orgReadState builds the collector-owned read record for one organization
// target claim (#8943): whether the org stream was cut short, how many
// allowlisted repositories of the organization the target covers, and which of
// them the fan-out did not read in full, each with the collection reason the
// facts of that repository carry. A repository read in full is absent, so the
// read path can report an empty answer for it as complete. orgTruncated is the
// org stream's own flag, not collection.truncated, which also turns on when a
// fan-out repository read fails.
func orgReadState(
	target TargetConfig,
	generationID string,
	observedAt time.Time,
	orgTruncated bool,
	collection alertCollection,
) securityalerts.TargetReadState {
	repositories := organizationFanoutRepositories(target)
	state := securityalerts.TargetReadState{
		GenerationID:      generationID,
		ScopeID:           target.ScopeID,
		OrgReadTruncated:  orgTruncated,
		RepositoriesTotal: len(repositories),
		ObservedAt:        observedAt,
		// The cursor commits with the generation (#8946): where this fan-out
		// started and where the next claim starts.
		FanoutStartRepository: collection.fanoutStartRepository,
		FanoutNextRepository:  collection.fanoutNextRepository,
		FanoutLapRepository:   collection.fanoutLapRepository,
	}
	for _, repository := range repositories {
		outcome, fannedOut := collection.fanoutOutcomes[repository]
		if !fannedOut || outcome == contract.SecurityAlertFanoutOutcomeComplete {
			continue
		}
		reason := fanoutOutcomeReason(outcome)
		if reason == "" {
			// A repository read that itself stopped at max_pages keeps the
			// page-limit reason its facts carry.
			reason = collectionOpenPageLimitReachedReason
		}
		state.Unread = append(state.Unread, securityalerts.UnreadRepository{Repository: repository, Reason: reason})
	}
	slices.SortFunc(state.Unread, func(a, b securityalerts.UnreadRepository) int {
		return strings.Compare(a.Repository, b.Repository)
	})
	return state
}

// readStateWriter returns the writer that persists the org read state with its
// generation, or nil when the target is not an organization or no writer
// factory is configured. It also stamps the org stream's truncation flag on the
// observe span; the unread count is the existing org_fanout.incomplete
// attribute. Neither carries a repository name.
func (s *ClaimedSource) readStateWriter(
	observeSpan trace.Span,
	target TargetConfig,
	generationID string,
	observedAt time.Time,
	orgTruncated bool,
	collection alertCollection,
) collector.GenerationStateWriter {
	if !target.IsOrganizationScope() || s.readStateFactory == nil {
		return nil
	}
	state := orgReadState(target, generationID, observedAt, orgTruncated, collection)
	if observeSpan != nil {
		observeSpan.SetAttributes(attribute.Bool(contract.AttrSecurityAlertOrgReadTruncated, state.OrgReadTruncated))
	}
	return s.readStateFactory(state)
}
