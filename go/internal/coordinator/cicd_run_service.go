// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // CI/CD scheduling and durable admission remain on root Service methods.

import (
	"context"
	"fmt"
	"time"

	cicdrun "github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// CICDRunPlanner plans CI/CD run workflow rows from collector instance
// configuration.
type CICDRunPlanner interface {
	PlanCICDRunWork(context.Context, cicdrun.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// scheduleCICDRunWork plans one claimable work item per enabled CI/CD run
// target. It only creates durable workflow rows; provider calls and fact
// emission remain owned by the CI/CD run collector runtime.
func (s Service) scheduleCICDRunWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleCICDRun(instance) {
			continue
		}
		if s.CICDRunPlanner == nil {
			return fmt.Errorf("ci/cd run planner is required for active ci_cd_run collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		planKey := scheduledPlanKey(instance, observedAt, interval)
		run, items, err := s.CICDRunPlanner.PlanCICDRunWork(ctx, cicdrun.PlanRequest{
			Instance: instance, ObservedAt: observedAt, PlanKey: planKey,
		})
		if err != nil {
			return fmt.Errorf("plan ci/cd run work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		githubRepositories, err := cicdrun.ConfiguredGitHubTargetRepositories(instance.Configuration)
		if err != nil {
			return fmt.Errorf("read ci/cd run repositories for %q: %w", instance.InstanceID, err)
		}
		githubRun, otherRun := run, run
		githubItems := make([]workflow.WorkItem, 0, len(items))
		otherItems := make([]workflow.WorkItem, 0, len(items))
		// A mixed instance needs separate durable run IDs. The store refuses to
		// append work after a run becomes terminal, and the first provider can
		// finish while this coordinator admits the second provider. Choose the
		// groups from configuration, before pending-event filtering, so a webhook
		// does not change the scheduled identities across ticks.
		mixedProviders := len(githubRepositories) > 0 && len(githubRepositories) < len(items)
		if mixedProviders {
			githubRun, githubItems, err = s.CICDRunPlanner.PlanCICDRunWork(ctx, cicdrun.PlanRequest{
				Instance: instance, ObservedAt: observedAt, PlanKey: planKey + "-github-actions", ScheduledProvider: "github_actions",
			})
			if err != nil {
				return fmt.Errorf("plan github actions scheduled group for %q: %w", instance.InstanceID, err)
			}
			otherRun, otherItems, err = s.CICDRunPlanner.PlanCICDRunWork(ctx, cicdrun.PlanRequest{
				Instance: instance, ObservedAt: observedAt, PlanKey: planKey + "-gitlab-ci", ScheduledProvider: "gitlab_ci",
			})
			if err != nil {
				return fmt.Errorf("plan gitlab ci scheduled group for %q: %w", instance.InstanceID, err)
			}
			if len(githubItems)+len(otherItems) != len(items) {
				return fmt.Errorf("ci/cd run provider groups do not cover configured targets for %q", instance.InstanceID)
			}
			if githubRun.RunID == otherRun.RunID {
				return fmt.Errorf("ci/cd run provider groups share one run id for %q", instance.InstanceID)
			}
		} else {
			for _, item := range items {
				if _, github := githubRepositories[item.ScopeID]; github {
					githubItems = append(githubItems, item)
				} else {
					otherItems = append(otherItems, item)
				}
			}
		}
		eligibleGithubItems := githubItems[:0]
		for _, item := range githubItems {
			repository, github := githubRepositories[item.ScopeID]
			if !github {
				return fmt.Errorf("github actions provider group includes unconfigured scope %q", item.ScopeID)
			}
			if s.CICDRunActionsTriggers != nil {
				pending, pendingErr := s.CICDRunActionsTriggers.HasPendingRepository(ctx, repository)
				if pendingErr != nil {
					return fmt.Errorf("read pending ci/cd webhook for %q: %w", repository, pendingErr)
				}
				if pending {
					continue
				}
			}
			eligibleGithubItems = append(eligibleGithubItems, item)
		}
		githubItems = eligibleGithubItems
		if len(githubItems) > 0 {
			inserted, admitErr := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, githubRun, githubItems)
			if admitErr != nil {
				return fmt.Errorf("create ci/cd run scheduled work for %q: %w", instance.InstanceID, admitErr)
			}
			if inserted > 0 && s.CICDRunInstruments != nil {
				s.CICDRunInstruments.CICDRunPollFallback.Add(ctx, int64(inserted))
			}
		}
		if len(otherItems) > 0 {
			if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, otherRun, otherItems); err != nil {
				return fmt.Errorf("create ci/cd run scheduled work for %q: %w", instance.InstanceID, err)
			}
		}
	}
	return nil
}

func shouldScheduleCICDRun(instance workflow.CollectorInstance) bool {
	return inTreeSchedulable(instance, scope.CollectorCICDRun)
}
