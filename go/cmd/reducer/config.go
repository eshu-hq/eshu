// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	reducerRetryDelayEnv                    = "ESHU_REDUCER_RETRY_DELAY"
	reducerMaxAttemptsEnv                   = "ESHU_REDUCER_MAX_ATTEMPTS"
	reducerWorkersEnv                       = "ESHU_REDUCER_WORKERS"
	reducerBatchClaimEnv                    = "ESHU_REDUCER_BATCH_CLAIM_SIZE"
	reducerClaimDomainEnv                   = "ESHU_REDUCER_CLAIM_DOMAIN"
	reducerClaimDomainsEnv                  = "ESHU_REDUCER_CLAIM_DOMAINS"
	queryProfileEnv                         = "ESHU_QUERY_PROFILE"
	reducerExpectedSourceLocalProjectorsEnv = "ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS"
	reducerSemanticEntityClaimLimitEnv      = "ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT"

	generationRetentionEnabledEnv                  = "ESHU_GENERATION_RETENTION_ENABLED"
	generationRetentionPollIntervalEnv             = "ESHU_GENERATION_RETENTION_POLL_INTERVAL"
	generationRetentionMinSupersededGenerationsEnv = "ESHU_GENERATION_RETENTION_MIN_SUPERSEDED_GENERATIONS"
	generationRetentionMaxSupersededAgeEnv         = "ESHU_GENERATION_RETENTION_MAX_SUPERSEDED_AGE"
	generationRetentionBatchGenerationLimitEnv     = "ESHU_GENERATION_RETENTION_BATCH_GENERATION_LIMIT"
	generationRetentionBatchRowLimitEnv            = "ESHU_GENERATION_RETENTION_BATCH_ROW_LIMIT"
	generationRetentionPolicyScopeEnv              = "ESHU_GENERATION_RETENTION_POLICY_SCOPE"
	generationRetentionPolicyRevisionEnv           = "ESHU_GENERATION_RETENTION_POLICY_REVISION"

	generationLivenessEnabledEnv            = "ESHU_GENERATION_LIVENESS_ENABLED"
	generationLivenessPollIntervalEnv       = "ESHU_GENERATION_LIVENESS_POLL_INTERVAL"
	generationLivenessActivationDeadlineEnv = "ESHU_GENERATION_LIVENESS_ACTIVATION_DEADLINE"
	generationLivenessMaxRecoverAttemptsEnv = "ESHU_GENERATION_LIVENESS_MAX_RECOVER_ATTEMPTS"
	generationLivenessBatchLimitEnv         = "ESHU_GENERATION_LIVENESS_BATCH_LIMIT"

	// poisonLivenessAutoRetryEnabledEnv opts into the bounded auto-retry sweep
	// for the dead-letter/poison class (#4740). Default is false: the poison
	// stuck-gauge is always active regardless of this flag, but the sweep loop
	// itself — and therefore any dead_letter -> pending re-enqueue write — only
	// runs when an operator explicitly opts in. This is the deliberate
	// surface-only default posture the ticket asked for.
	poisonLivenessAutoRetryEnabledEnv   = "ESHU_POISON_LIVENESS_AUTO_RETRY_ENABLED"
	poisonLivenessPollIntervalEnv       = "ESHU_POISON_LIVENESS_POLL_INTERVAL"
	poisonLivenessMaxRecoverAttemptsEnv = "ESHU_POISON_LIVENESS_MAX_RECOVER_ATTEMPTS"
	poisonLivenessBatchLimitEnv         = "ESHU_POISON_LIVENESS_BATCH_LIMIT"

	graphOrphanSweepEnabledEnv      = "ESHU_GRAPH_ORPHAN_SWEEP_ENABLED"
	graphOrphanSweepPollIntervalEnv = "ESHU_GRAPH_ORPHAN_SWEEP_POLL_INTERVAL"
	graphOrphanSweepTTLEnv          = "ESHU_GRAPH_ORPHAN_SWEEP_TTL"
	graphOrphanSweepBatchLimitEnv   = "ESHU_GRAPH_ORPHAN_SWEEP_BATCH_LIMIT"
	graphOrphanSweepCountLimitEnv   = "ESHU_GRAPH_ORPHAN_SWEEP_COUNT_LIMIT"
	graphOrphanSweepLeaseOwnerEnv   = "ESHU_GRAPH_ORPHAN_SWEEP_LEASE_OWNER"
	graphOrphanSweepLeaseTTLEnv     = "ESHU_GRAPH_ORPHAN_SWEEP_LEASE_TTL"

	driftPriorConfigDepthEnv = "ESHU_DRIFT_PRIOR_CONFIG_DEPTH"

	defaultGenerationRetentionPollInterval = time.Hour

	defaultGenerationLivenessPollInterval       = 5 * time.Minute
	defaultGenerationLivenessActivationDeadline = 30 * time.Minute
	defaultGenerationLivenessMaxRecoverAttempts = 5
	defaultGenerationLivenessBatchLimit         = 200

	defaultPoisonLivenessPollInterval       = 5 * time.Minute
	defaultPoisonLivenessMaxRecoverAttempts = 1
	defaultPoisonLivenessBatchLimit         = 200

	defaultGraphOrphanSweepPollInterval = time.Hour
	defaultGraphOrphanSweepTTL          = 7 * 24 * time.Hour
	defaultGraphOrphanSweepBatchLimit   = 100
	defaultGraphOrphanSweepCountLimit   = 10_000
	defaultGraphOrphanSweepLeaseTTL     = 5 * time.Minute
)

type generationRetentionConfig struct {
	Enabled bool
	Runner  reducer.GenerationRetentionRunnerConfig
}

type generationLivenessConfig struct {
	Enabled bool
	Runner  reducer.GenerationLivenessRunnerConfig
}

// poisonLivenessConfig configures the #4740 dead-letter/poison bounded
// recovery arm. The runner is only constructed (see poisonLivenessRunnerFor)
// when Runner.AutoRetryEnabled is true; the stuck-gauge is wired independently
// in registerReducerObservableGauges and is always active.
type poisonLivenessConfig struct {
	Runner reducer.PoisonLivenessRunnerConfig
}

type graphOrphanSweepConfig struct {
	Enabled bool
	Runner  reducer.GraphOrphanSweepRunnerConfig
}

func loadReducerQueueConfig(getenv func(string) string) (runtimecfg.RetryPolicyConfig, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	return runtimecfg.LoadRetryPolicyConfig(getenv, "REDUCER")
}

func loadReducerBatchClaimSize(getenv func(string) string, workers int, graphBackend runtimecfg.GraphBackend) int {
	if raw := strings.TrimSpace(getenv(reducerBatchClaimEnv)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		return workers
	}
	n := workers * 4
	if n > 64 {
		n = 64
	}
	if n < 4 {
		n = 4
	}
	return n
}

func loadReducerClaimDomain(getenv func(string) string) (reducer.Domain, error) {
	domains, err := loadReducerClaimDomains(getenv)
	if err != nil {
		return "", err
	}
	if len(domains) == 0 {
		return "", nil
	}
	if len(domains) > 1 {
		return "", fmt.Errorf("%s supports exactly one reducer domain; set %s for multiple domains and wire the plural claim-domain configuration", reducerClaimDomainEnv, reducerClaimDomainsEnv)
	}
	return domains[0], nil
}

func loadReducerClaimDomains(getenv func(string) string) ([]reducer.Domain, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	legacyRaw := strings.TrimSpace(getenv(reducerClaimDomainEnv))
	raw := strings.TrimSpace(getenv(reducerClaimDomainsEnv))
	sourceEnv := reducerClaimDomainsEnv
	if legacyRaw != "" && raw != "" {
		return nil, fmt.Errorf("%s and %s cannot both be set", reducerClaimDomainEnv, reducerClaimDomainsEnv)
	}
	if raw == "" {
		raw = legacyRaw
		sourceEnv = reducerClaimDomainEnv
	}
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	domains := make([]reducer.Domain, 0, len(parts))
	seen := make(map[reducer.Domain]struct{}, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			return nil, fmt.Errorf("%s contains an empty reducer domain", sourceEnv)
		}
		domain, err := reducer.ParseDomain(value)
		if err != nil {
			return nil, fmt.Errorf("%s contains invalid reducer domain %q: %w", sourceEnv, value, err)
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}
	return domains, nil
}

func loadReducerExpectedSourceLocalProjectors(getenv func(string) string) int {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	raw := strings.TrimSpace(getenv(reducerExpectedSourceLocalProjectorsEnv))
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func loadReducerSemanticEntityClaimLimit(
	getenv func(string) string,
	_ runtimecfg.GraphBackend,
) int {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	raw := strings.TrimSpace(getenv(reducerSemanticEntityClaimLimitEnv))
	if raw != "" {
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func loadReducerProjectorDrainGate(
	getenv func(string) string,
	graphBackend runtimecfg.GraphBackend,
) (bool, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	profile, err := query.ParseQueryProfile(getenv(queryProfileEnv))
	if err != nil {
		return false, err
	}
	return graphBackend == runtimecfg.GraphBackendNornicDB &&
		profile == query.ProfileLocalAuthoritative, nil
}

func loadReducerWorkerCount(getenv func(string) string, graphBackend runtimecfg.GraphBackend) int {
	if raw := strings.TrimSpace(getenv(reducerWorkersEnv)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		n := cpubudget.UsableCPUs()
		if n < 1 {
			n = 1
		}
		return n
	}
	n := cpubudget.UsableCPUs()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}

func loadGenerationRetentionConfig(getenv func(string) string) generationRetentionConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	defaults := postgres.DefaultGenerationRetentionPolicy()
	return generationRetentionConfig{
		Enabled: loadBoolOrDefault(getenv, generationRetentionEnabledEnv, true),
		Runner: reducer.GenerationRetentionRunnerConfig{
			PollInterval: loadDurationOrDefault(getenv, generationRetentionPollIntervalEnv, defaultGenerationRetentionPollInterval),
			Policy: reducer.GenerationRetentionPolicy{
				MinSupersededGenerations: loadPositiveIntOrDefault(getenv, generationRetentionMinSupersededGenerationsEnv, defaults.MinSupersededGenerations),
				MaxSupersededAge:         loadDurationOrDefault(getenv, generationRetentionMaxSupersededAgeEnv, defaults.MaxSupersededAge),
				BatchGenerationLimit:     loadPositiveIntOrDefault(getenv, generationRetentionBatchGenerationLimitEnv, defaults.BatchGenerationLimit),
				BatchRowLimit:            loadPositiveIntOrDefault(getenv, generationRetentionBatchRowLimitEnv, defaults.BatchRowLimit),
				PolicyScope:              loadStringOrDefault(getenv, generationRetentionPolicyScopeEnv, defaults.PolicyScope),
				PolicyRevision:           loadStringOrDefault(getenv, generationRetentionPolicyRevisionEnv, defaults.PolicyRevision),
			},
		},
	}
}

func loadGenerationLivenessConfig(getenv func(string) string) generationLivenessConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return generationLivenessConfig{
		Enabled: loadBoolOrDefault(getenv, generationLivenessEnabledEnv, true),
		Runner: reducer.GenerationLivenessRunnerConfig{
			PollInterval: loadDurationOrDefault(getenv, generationLivenessPollIntervalEnv, defaultGenerationLivenessPollInterval),
			Policy: reducer.GenerationLivenessPolicy{
				ActivationDeadline: loadDurationOrDefault(getenv, generationLivenessActivationDeadlineEnv, defaultGenerationLivenessActivationDeadline),
				MaxRecoverAttempts: loadPositiveIntOrDefault(getenv, generationLivenessMaxRecoverAttemptsEnv, defaultGenerationLivenessMaxRecoverAttempts),
				BatchLimit:         loadPositiveIntOrDefault(getenv, generationLivenessBatchLimitEnv, defaultGenerationLivenessBatchLimit),
			},
		},
	}
}

func loadPoisonLivenessConfig(getenv func(string) string) poisonLivenessConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return poisonLivenessConfig{
		Runner: reducer.PoisonLivenessRunnerConfig{
			AutoRetryEnabled: loadBoolOrDefault(getenv, poisonLivenessAutoRetryEnabledEnv, false),
			PollInterval:     loadDurationOrDefault(getenv, poisonLivenessPollIntervalEnv, defaultPoisonLivenessPollInterval),
			Policy: reducer.PoisonLivenessPolicy{
				MaxRecoverAttempts: loadPositiveIntOrDefault(getenv, poisonLivenessMaxRecoverAttemptsEnv, defaultPoisonLivenessMaxRecoverAttempts),
				BatchLimit:         loadPositiveIntOrDefault(getenv, poisonLivenessBatchLimitEnv, defaultPoisonLivenessBatchLimit),
			},
		},
	}
}

func validateGenerationRetentionConfig(
	getenv func(string) string,
	cfg generationRetentionConfig,
) error {
	if cfg.Enabled {
		return nil
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	profile, err := query.ParseQueryProfile(getenv(queryProfileEnv))
	if err != nil {
		return err
	}
	switch profile {
	case query.ProfileLocalLightweight, query.ProfileLocalAuthoritative, query.ProfileLocalFullStack:
		return nil
	default:
		return fmt.Errorf("%s=false requires an explicit local %s profile; production reducers must run generation retention", generationRetentionEnabledEnv, queryProfileEnv)
	}
}

func loadGraphOrphanSweepConfig(getenv func(string) string) graphOrphanSweepConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return graphOrphanSweepConfig{
		Enabled: loadBoolOrDefault(getenv, graphOrphanSweepEnabledEnv, true),
		Runner: reducer.GraphOrphanSweepRunnerConfig{
			PollInterval: loadDurationOrDefault(getenv, graphOrphanSweepPollIntervalEnv, defaultGraphOrphanSweepPollInterval),
			LeaseOwner:   loadStringOrDefault(getenv, graphOrphanSweepLeaseOwnerEnv, defaultGraphOrphanSweepLeaseOwner()),
			LeaseTTL:     loadDurationOrDefault(getenv, graphOrphanSweepLeaseTTLEnv, defaultGraphOrphanSweepLeaseTTL),
			Policy: reducer.GraphOrphanSweepPolicy{
				OrphanTTL:  loadDurationOrDefault(getenv, graphOrphanSweepTTLEnv, defaultGraphOrphanSweepTTL),
				BatchLimit: loadPositiveIntOrDefault(getenv, graphOrphanSweepBatchLimitEnv, defaultGraphOrphanSweepBatchLimit),
				CountLimit: loadPositiveIntOrDefault(getenv, graphOrphanSweepCountLimitEnv, defaultGraphOrphanSweepCountLimit),
				Labels:     defaultGraphOrphanSweepLabels(),
			},
		},
	}
}

func defaultGraphOrphanSweepLabels() []string {
	labels := sourcecypher.DefaultOrphanSweepLabels()
	values := make([]string, 0, len(labels))
	for _, label := range labels {
		values = append(values, string(label))
	}
	return values
}

func defaultGraphOrphanSweepLeaseOwner() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("graph-orphan-sweep-runner:%s:%d", hostname, os.Getpid())
}

// defaultSupplyChainImpactWinnersLeaseOwner derives a per-process lease owner for
// the impact canonical winners maintainer (#3389). A unique owner per pod/process
// is required: the shared partition-lease SQL treats a live lease with the same
// owner as re-claimable, so a shared default would let every resolution-engine
// instance resweep the winners table concurrently each cadence, defeating the
// single-owner guard.
func defaultSupplyChainImpactWinnersLeaseOwner() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("supply-chain-impact-winners-maintainer:%s:%d", hostname, os.Getpid())
}

// defaultCollectorEvidenceSummaryLeaseOwner builds a per-instance lease owner for
// the #3466 collector-evidence-summary maintainer. As with the winners maintainer,
// a per-instance owner is required: the shared partition-lease SQL treats a live
// lease with the same owner as re-claimable, so a shared default would let every
// reducer instance resweep the summary table concurrently each cadence, defeating
// the single-owner guard.
func defaultCollectorEvidenceSummaryLeaseOwner() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("collector-evidence-summary-maintainer:%s:%d", hostname, os.Getpid())
}

func loadStringOrDefault(getenv func(string) string, key string, defaultValue string) string {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	return raw
}

func loadDurationOrDefault(getenv func(string) string, key string, defaultValue time.Duration) time.Duration {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}

func loadPositiveIntOrDefault(getenv func(string) string, key string, defaultValue int) int {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}

func loadBoolOrDefault(getenv func(string) string, key string, defaultValue bool) bool {
	raw := strings.TrimSpace(strings.ToLower(getenv(key)))
	switch raw {
	case "":
		return defaultValue
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return defaultValue
	}
}

// identityCacheMaxBytes reads ESHU_IDENTITY_CACHE_MAX_BYTES. Zero or unset
// uses the evidence-based default (500 MiB). Negative disables the cache
// entirely (returns -1 so NewIdentityEpochCache returns nil).
func identityCacheMaxBytes(getenv func(string) string) int64 {
	raw := strings.TrimSpace(getenv("ESHU_IDENTITY_CACHE_MAX_BYTES"))
	if raw == "" {
		return 0 // use default
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || val == 0 {
		return 0 // use default
	}
	return val // negative → disable, positive → override
}

// parsePriorConfigDepth converts the ESHU_DRIFT_PRIOR_CONFIG_DEPTH env value
// into the loader's bound. Empty input and explicit "0" both return 0 (the
// loader interprets 0 as "use defaultPriorConfigDepth", currently 10).
// Negative or non-integer values emit a WARN log and also fall back to 0 so
// a typo cannot disable drift detection — operator error is observable but
// non-fatal.
func parsePriorConfigDepth(raw string, logger *slog.Logger) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		if logger != nil {
			logger.LogAttrs(
				context.Background(), slog.LevelWarn,
				"invalid ESHU_DRIFT_PRIOR_CONFIG_DEPTH; falling back to default",
				slog.String("raw", raw),
				slog.String(telemetry.LogKeyFailureClass, "env_parse"),
			)
		}
		return 0
	}
	return n
}
