// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun"
	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/ghactionsruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/gitlabciruntime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_CICD_RUN_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_CICD_RUN_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_CICD_RUN_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_CICD_RUN_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
)

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	// GitHubSource and GitLabSource are built from the SAME instance
	// configuration's targets, split by each target's declared provider
	// (targetJSON.Provider). One or the other may have zero Targets when the
	// instance configures only a single provider; buildClaimedService (service.go)
	// constructs a claim-aware source for a provider only when its Targets
	// slice is non-empty, then routes claims to the right one by scope_id
	// through providerRoutedSource (provider_source.go).
	GitHubSource ghactionsruntime.SourceConfig
	GitLabSource gitlabciruntime.SourceConfig
}

type cicdRunRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

// targetJSON is one configured CI/CD target. Provider selects which runtime
// (ghactionsruntime or gitlabciruntime) parseCICDRunRuntimeConfiguration
// builds this target under: "github_actions" (the default when omitted, for
// backward compatibility with configuration that predates provider dispatch)
// or "gitlab_ci". Repository/AllowedRepositories carry the provider's own
// locator shape either way -- "owner/repo" for GitHub Actions,
// "namespace/project" (or "group/subgroup/project") for GitLab CI -- and
// MaxArtifacts is GitHub-Actions-only (GitLab reports job artifacts inline
// with no separate paginated endpoint; see gitlabciruntime's README.md), so
// it is silently ignored for a gitlab_ci target rather than rejected.
type targetJSON struct {
	Provider            string   `json:"provider"`
	ScopeID             string   `json:"scope_id"`
	Repository          string   `json:"repository"`
	TokenEnv            string   `json:"token_env"`
	AllowedRepositories []string `json:"allowed_repositories"`
	APIBaseURL          string   `json:"api_base_url"`
	MaxRuns             int      `json:"max_runs"`
	MaxJobs             int      `json:"max_jobs"`
	MaxArtifacts        int      `json:"max_artifacts"`
	SourceURI           string   `json:"source_uri"`
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectCICDRunInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateCICDRunInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	githubSource, gitlabSource, err := parseCICDRunRuntimeConfiguration(instance, getenv)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, envPollInterval, time.Second)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, envClaimLeaseTTL, workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	heartbeatInterval, err := envDuration(getenv, envHeartbeatInterval, workflow.DefaultHeartbeatInterval())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if heartbeatInterval >= claimLeaseTTL {
		return claimedRuntimeConfig{}, fmt.Errorf("ci/cd run collector heartbeat interval must be less than claim lease TTL")
	}
	return claimedRuntimeConfig{
		Instance:          instance,
		OwnerID:           ownerID(getenv),
		PollInterval:      pollInterval,
		ClaimLeaseTTL:     claimLeaseTTL,
		HeartbeatInterval: heartbeatInterval,
		GitHubSource:      githubSource,
		GitLabSource:      gitlabSource,
	}, nil
}

func selectCICDRunInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorCICDRun {
			continue
		}
		if requestedInstanceID != "" && instance.InstanceID != requestedInstanceID {
			continue
		}
		matches = append(matches, instance)
	}
	switch len(matches) {
	case 0:
		if requestedInstanceID != "" {
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("ci/cd run collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no ci/cd run collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple ci/cd run collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateCICDRunInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("ci/cd run collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorCICDRun {
		return fmt.Errorf("ci/cd run collector requires collector_kind %q", scope.CollectorCICDRun)
	}
	if !instance.Enabled {
		return fmt.Errorf("ci/cd run collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("ci/cd run collector requires claim-enabled collector instance")
	}
	if err := workflow.ValidateCICDRunCollectorConfiguration(instance.Configuration); err != nil {
		return fmt.Errorf("ci/cd run collector configuration: %w", err)
	}
	return nil
}

// parseCICDRunRuntimeConfiguration decodes the instance's target list and
// splits it into a ghactionsruntime.SourceConfig and a
// gitlabciruntime.SourceConfig by each target's declared provider. An
// omitted or blank provider defaults to github_actions (backward
// compatible with configuration that predates provider dispatch, issue
// #5427 -- every existing deployed target predates the provider field ever
// being read). Either returned SourceConfig may have zero Targets when the
// instance configures only one provider; buildClaimedService (service.go)
// only constructs a claim-aware source for a provider whose Targets slice
// is non-empty.
func parseCICDRunRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (ghactionsruntime.SourceConfig, gitlabciruntime.SourceConfig, error) {
	var decoded cicdRunRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return ghactionsruntime.SourceConfig{}, gitlabciruntime.SourceConfig{}, fmt.Errorf("decode ci/cd run collector configuration: %w", err)
	}
	githubTargets := make([]ghactionsruntime.TargetConfig, 0, len(decoded.Targets))
	gitlabTargets := make([]gitlabciruntime.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		switch provider := strings.TrimSpace(target.Provider); provider {
		case "", string(cicdrun.ProviderGitHubActions):
			mapped, err := mapTarget(target, getenv)
			if err != nil {
				return ghactionsruntime.SourceConfig{}, gitlabciruntime.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
			}
			githubTargets = append(githubTargets, mapped)
		case string(cicdrun.ProviderGitLabCI):
			mapped, err := mapGitLabTarget(target, getenv)
			if err != nil {
				return ghactionsruntime.SourceConfig{}, gitlabciruntime.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
			}
			gitlabTargets = append(gitlabTargets, mapped)
		default:
			return ghactionsruntime.SourceConfig{}, gitlabciruntime.SourceConfig{}, fmt.Errorf("targets[%d]: unsupported provider %q", i, provider)
		}
	}
	githubSource := ghactionsruntime.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Client:              ghactionsruntime.GitHubClient{},
		Targets:             githubTargets,
	}
	gitlabSource := gitlabciruntime.SourceConfig{
		CollectorInstanceID: instance.InstanceID,
		Client:              gitlabciruntime.GitLabClient{},
		Targets:             gitlabTargets,
	}
	return githubSource, gitlabSource, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (ghactionsruntime.TargetConfig, error) {
	tokenEnv := strings.TrimSpace(target.TokenEnv)
	token := ""
	if tokenEnv != "" {
		token = strings.TrimSpace(getenv(tokenEnv))
	}
	if token == "" {
		return ghactionsruntime.TargetConfig{}, fmt.Errorf("token_env %s did not resolve a credential", tokenEnv)
	}
	return ghactionsruntime.TargetConfig{
		ScopeID:             strings.TrimSpace(target.ScopeID),
		Repository:          strings.Trim(target.Repository, "/"),
		Token:               token,
		AllowedRepositories: cleanConfigStrings(target.AllowedRepositories),
		APIBaseURL:          strings.TrimRight(strings.TrimSpace(target.APIBaseURL), "/"),
		SourceURI:           strings.TrimSpace(firstNonBlank(target.SourceURI, "https://github.com/"+strings.Trim(target.Repository, "/"))),
		MaxRuns:             target.MaxRuns,
		MaxJobs:             target.MaxJobs,
		MaxArtifacts:        target.MaxArtifacts,
	}, nil
}

// mapGitLabTarget is mapTarget's gitlab_ci counterpart: target.Repository
// carries the GitLab project path ("namespace/project", or
// "group/subgroup/project" for a nested subgroup) in the same JSON field
// GitHub Actions targets use for "owner/repo", and target.AllowedRepositories
// carries the matching allow-list. There is no GitLab counterpart to
// MaxArtifacts (GitLab reports job artifacts inline; see
// gitlabciruntime's README.md), so it is not read here.
func mapGitLabTarget(target targetJSON, getenv func(string) string) (gitlabciruntime.TargetConfig, error) {
	tokenEnv := strings.TrimSpace(target.TokenEnv)
	token := ""
	if tokenEnv != "" {
		token = strings.TrimSpace(getenv(tokenEnv))
	}
	if token == "" {
		return gitlabciruntime.TargetConfig{}, fmt.Errorf("token_env %s did not resolve a credential", tokenEnv)
	}
	return gitlabciruntime.TargetConfig{
		ScopeID:             strings.TrimSpace(target.ScopeID),
		ProjectPath:         strings.Trim(target.Repository, "/"),
		Token:               token,
		AllowedProjectPaths: cleanConfigStrings(target.AllowedRepositories),
		APIBaseURL:          strings.TrimRight(strings.TrimSpace(target.APIBaseURL), "/"),
		SourceURI:           strings.TrimSpace(firstNonBlank(target.SourceURI, "https://gitlab.com/"+strings.Trim(target.Repository, "/"))),
		MaxRuns:             target.MaxRuns,
		MaxJobs:             target.MaxJobs,
	}, nil
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return value, nil
}

func ownerID(getenv func(string) string) string {
	for _, key := range []string{envOwnerID, "HOSTNAME"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return "collector-cicd-run"
}

func cleanConfigStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
