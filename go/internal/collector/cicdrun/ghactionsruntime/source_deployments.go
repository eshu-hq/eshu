// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// This file holds #5425 STEP 3's GitHub Deployments API wiring: fetching a
// target's deployment window and normalizing it into facts. It is split out
// of source.go (486 lines before this change, at the repo's 500-line cap)
// rather than growing that file further.

const (
	// defaultMaxDeployments bounds a target's deployment window with the
	// DEFAULT, mirroring defaultMaxRuns' rationale in source.go: an
	// omitted/zero max_deployments resolves to this value, and steady-state
	// per-repo request volume tracks the actual new-deployment rate (an
	// unchanged deployment_status re-emits idempotently at projection, same
	// as an unchanged run).
	defaultMaxDeployments = 10
	// maxDeploymentPages is the hard cap validateTarget enforces for a
	// target that opts into a wider window explicitly, mirroring
	// maxRunPages.
	maxDeploymentPages = 100
)

// boundMaxDeployments applies the default/range validation for
// TargetConfig.MaxDeployments, split out of validateTarget in source.go (at
// the repo's 500-line cap) rather than growing that function further. An
// omitted/zero value resolves to defaultMaxDeployments, mirroring how
// validateTarget defaults MaxRuns.
func boundMaxDeployments(target TargetConfig) (TargetConfig, error) {
	if target.MaxDeployments == 0 {
		target.MaxDeployments = defaultMaxDeployments
	}
	if target.MaxDeployments < 0 || target.MaxDeployments > maxDeploymentPages {
		return TargetConfig{}, fmt.Errorf("max_deployments must be between 0 and %d (0 uses the default of %d)", maxDeploymentPages, defaultMaxDeployments)
	}
	return target, nil
}

// DeploymentFetcher is the optional Client capability for fetching GitHub's
// Deployments API. It is deliberately NOT folded into the Client interface
// in source.go: this package's existing test doubles (fakeClient and its
// variants across source_test.go, source_multi_run_test.go,
// source_watermark_test.go, pending_watermark_test.go) only exercise the
// FetchRuns path, and requiring every one of them to also implement
// FetchDeployments would be unrelated churn on every existing run-collection
// test. Only GitHubClient (the real production client, client_deployments.go)
// and test doubles that specifically test deployment collection implement
// it; appendDeploymentEnvelopes type-asserts for it and simply skips
// deployment collection when the configured Client does not implement it.
type DeploymentFetcher interface {
	FetchDeployments(context.Context, TargetConfig) (DeploymentPage, error)
}

// appendDeploymentEnvelopes fetches this target's bounded deployment window
// (when the configured Client implements DeploymentFetcher) and appends its
// normalized ci.deployment_event and ci.warning facts onto envelopes so they
// land in the SAME CollectedGeneration as the ci.run facts NextClaimed
// already built -- see the call site in source.go for why that generation
// boundary is load-bearing. When the Client has no deployment-fetching
// capability, envelopes is returned unchanged, truncated is false, and no
// facts are added. The provider request/fetch-duration/rate-limit signal is
// recorded through the SAME shared instruments (recordFetch/recordRateLimit)
// the run fetch in NextClaimed uses, labeled under the same
// provider=github_actions dimension: this is more requests against the same
// provider account, not a new signal surface.
func (s ClaimedSource) appendDeploymentEnvelopes(
	ctx context.Context,
	observeSpan trace.Span,
	item workflow.WorkItem,
	target TargetConfig,
	runPage RunPage,
	envelopes []facts.Envelope,
	observedAt time.Time,
) ([]facts.Envelope, bool, error) {
	fetcher, ok := s.client.(DeploymentFetcher)
	if !ok {
		return envelopes, false, nil
	}
	startedAt := time.Now()
	page, err := fetcher.FetchDeployments(ctx, target)
	if err != nil {
		statusClass := classifyProviderStatus(err)
		s.recordFetch(ctx, statusClass, startedAt)
		s.recordRateLimit(ctx, statusClass)
		recordSpanError(observeSpan, err)
		return nil, false, err
	}
	s.recordFetch(ctx, "success", startedAt)

	deploymentCtx := cicdrun.FixtureContext{
		ScopeID:             item.ScopeID,
		GenerationID:        item.GenerationID,
		CollectorInstanceID: s.collectorInstanceID,
		FencingToken:        item.CurrentFencingToken,
		ObservedAt:          observedAt,
		SourceURI:           target.SourceURI,
		Repository:          target.Repository,
	}
	raw, err := json.Marshal(map[string]any{"deployments": deploymentFixtureRows(page.Snapshots)})
	if err != nil {
		recordSpanError(observeSpan, err)
		return nil, false, fmt.Errorf("marshal github deployments snapshot: %w", err)
	}
	deploymentEnvelopes, err := cicdrun.GitHubActionsDeploymentEnvelopes(raw, deploymentCtx)
	if err != nil {
		recordSpanError(observeSpan, err)
		return nil, false, fmt.Errorf("normalize github deployments snapshot: %w", err)
	}
	if page.Truncated {
		warning, warningErr := cicdrun.GitHubActionsDeploymentWarningEnvelope(deploymentCtx, "deployments:partial", "deployments_truncated",
			"additional deployments exist beyond the collected window; increase max_deployments or rely on idempotent re-collection to catch up")
		if warningErr != nil {
			recordSpanError(observeSpan, warningErr)
			return nil, false, warningErr
		}
		deploymentEnvelopes = append(deploymentEnvelopes, warning)
	}
	unanchored, err := unanchoredDeploymentWarnings(deploymentCtx, deploymentEnvelopes, runHeadSHAs(runPage))
	if err != nil {
		recordSpanError(observeSpan, err)
		return nil, false, err
	}
	envelopes = append(envelopes, deploymentEnvelopes...)
	envelopes = append(envelopes, unanchored...)
	return envelopes, page.Truncated, nil
}

// deploymentFixtureRows reshapes a fetched DeploymentPage's snapshots into
// the {"deployment":..., "statuses":...} row shape
// cicdrun.GitHubActionsDeploymentEnvelopes decodes, mirroring how
// buildRunEnvelopes marshals RunSnapshot into githubActionsFixture's shape
// for cicdrun.GitHubActionsFixtureEnvelopes.
func deploymentFixtureRows(snapshots []DeploymentSnapshot) []map[string]any {
	rows := make([]map[string]any, 0, len(snapshots))
	for _, snapshot := range snapshots {
		rows = append(rows, map[string]any{
			"deployment": snapshot.Deployment,
			"statuses":   snapshot.Statuses,
		})
	}
	return rows
}

// runHeadSHAs collects the distinct head_sha values across every run
// fetched in this same claim window, so unanchoredDeploymentWarnings can
// detect a deployment event whose sha matches none of them -- the same
// condition the reducer's attachDeploymentEventsToRuns
// (ci_cd_run_correlation_deploy_events.go) treats as "no run to attach to",
// just detected here at collection time so it is visible rather than
// silently inert downstream.
func runHeadSHAs(page RunPage) map[string]struct{} {
	shas := make(map[string]struct{}, len(page.Snapshots))
	for _, snapshot := range page.Snapshots {
		if sha, ok := snapshot.Run["head_sha"].(string); ok {
			if trimmed := strings.TrimSpace(sha); trimmed != "" {
				shas[trimmed] = struct{}{}
			}
		}
	}
	return shas
}

// unanchoredDeploymentWarnings emits one ci.warning per ci.deployment_event
// envelope whose sha matches none of this claim's fetched run head shas. It
// is expected, not exceptional -- deployments can predate or postdate the
// bounded run window, or be created outside GitHub Actions entirely -- but
// must stay visible rather than silently inert, since the reducer only
// attaches deployment evidence to a run sharing that sha.
func unanchoredDeploymentWarnings(
	ctx cicdrun.FixtureContext,
	deploymentEnvelopes []facts.Envelope,
	anchors map[string]struct{},
) ([]facts.Envelope, error) {
	var warnings []facts.Envelope
	for _, envelope := range deploymentEnvelopes {
		if envelope.FactKind != facts.CICDDeploymentEventFactKind {
			continue
		}
		sha, _ := envelope.Payload["sha"].(string)
		if _, ok := anchors[strings.TrimSpace(sha)]; ok {
			continue
		}
		deploymentID, _ := envelope.Payload["deployment_id"].(string)
		statusID, _ := envelope.Payload["status_id"].(string)
		warning, err := cicdrun.GitHubActionsDeploymentWarningEnvelope(ctx,
			"deployment:"+deploymentID+":"+statusID,
			"deployment_unanchored",
			"deployment event sha matched no fetched workflow run head sha")
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, warning)
	}
	return warnings, nil
}
