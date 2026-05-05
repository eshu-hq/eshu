package main

import (
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

const (
	reducerRetryDelayEnv                    = "ESHU_REDUCER_RETRY_DELAY"
	reducerMaxAttemptsEnv                   = "ESHU_REDUCER_MAX_ATTEMPTS"
	reducerWorkersEnv                       = "ESHU_REDUCER_WORKERS"
	reducerBatchClaimEnv                    = "ESHU_REDUCER_BATCH_CLAIM_SIZE"
	reducerClaimDomainEnv                   = "ESHU_REDUCER_CLAIM_DOMAIN"
	queryProfileEnv                         = "ESHU_QUERY_PROFILE"
	reducerExpectedSourceLocalProjectorsEnv = "ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS"
	reducerSemanticEntityClaimLimitEnv      = "ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT"

	codeCallProjectionPollIntervalEnv        = "ESHU_CODE_CALL_PROJECTION_POLL_INTERVAL"
	codeCallProjectionLeaseTTLEnv            = "ESHU_CODE_CALL_PROJECTION_LEASE_TTL"
	codeCallProjectionBatchLimitEnv          = "ESHU_CODE_CALL_PROJECTION_BATCH_LIMIT"
	codeCallProjectionAcceptanceScanLimitEnv = "ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT"
	codeCallProjectionLeaseOwnerEnv          = "ESHU_CODE_CALL_PROJECTION_LEASE_OWNER"
	repoDependencyProjectionPollIntervalEnv  = "ESHU_REPO_DEPENDENCY_PROJECTION_POLL_INTERVAL"
	repoDependencyProjectionLeaseTTLEnv      = "ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_TTL"
	repoDependencyProjectionBatchLimitEnv    = "ESHU_REPO_DEPENDENCY_PROJECTION_BATCH_LIMIT"
	repoDependencyProjectionLeaseOwnerEnv    = "ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_OWNER"
	codeCallEdgeBatchSizeEnv                 = "ESHU_CODE_CALL_EDGE_BATCH_SIZE"
	codeCallEdgeGroupBatchSizeEnv            = "ESHU_CODE_CALL_EDGE_GROUP_BATCH_SIZE"
	inheritanceEdgeGroupBatchSizeEnv         = "ESHU_INHERITANCE_EDGE_GROUP_BATCH_SIZE"
	sqlRelationshipEdgeGroupBatchSizeEnv     = "ESHU_SQL_RELATIONSHIP_EDGE_GROUP_BATCH_SIZE"

	graphProjectionRepairPollIntervalEnv = "ESHU_GRAPH_PROJECTION_REPAIR_POLL_INTERVAL"
	graphProjectionRepairBatchLimitEnv   = "ESHU_GRAPH_PROJECTION_REPAIR_BATCH_LIMIT"
	graphProjectionRepairRetryDelayEnv   = "ESHU_GRAPH_PROJECTION_REPAIR_RETRY_DELAY"

	defaultCodeCallProjectionPollInterval        = 500 * time.Millisecond
	defaultCodeCallProjectionLeaseTTL            = 60 * time.Second
	defaultCodeCallProjectionBatchLimit          = 100
	defaultCodeCallProjectionAcceptanceScanLimit = reducer.DefaultCodeCallAcceptanceScanLimit
	defaultCodeCallProjectionLeaseOwner          = "code-call-projection-runner"
	defaultRepoDependencyProjectionPollInterval  = 500 * time.Millisecond
	defaultRepoDependencyProjectionLeaseTTL      = 60 * time.Second
	defaultRepoDependencyProjectionBatchLimit    = 100
	defaultRepoDependencyProjectionLeaseOwner    = "repo-dependency-projection-runner"
	defaultCodeCallEdgeBatchSize                 = 1000
	defaultCodeCallEdgeGroupBatchSize            = 1
	defaultInheritanceEdgeGroupBatchSize         = 1
	defaultSQLRelationshipEdgeGroupBatchSize     = 1

	defaultGraphProjectionRepairPollInterval = time.Second
	defaultGraphProjectionRepairBatchLimit   = 100
	defaultGraphProjectionRepairRetryDelay   = time.Minute
)

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
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	raw := strings.TrimSpace(getenv(reducerClaimDomainEnv))
	if raw == "" {
		return "", nil
	}
	domain, err := reducer.ParseDomain(raw)
	if err != nil {
		return "", err
	}
	return domain, nil
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
	graphBackend runtimecfg.GraphBackend,
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
	if graphBackend == runtimecfg.GraphBackendNornicDB {
		return 1
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
		n := runtime.NumCPU()
		if n > 8 {
			n = 8
		}
		if n < 1 {
			n = 1
		}
		return n
	}
	n := runtime.NumCPU()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}

func loadCodeCallProjectionConfig(getenv func(string) string) reducer.CodeCallProjectionRunnerConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	return reducer.CodeCallProjectionRunnerConfig{
		LeaseOwner:          loadStringOrDefault(getenv, codeCallProjectionLeaseOwnerEnv, defaultCodeCallProjectionLeaseOwner),
		PollInterval:        loadDurationOrDefault(getenv, codeCallProjectionPollIntervalEnv, defaultCodeCallProjectionPollInterval),
		LeaseTTL:            loadDurationOrDefault(getenv, codeCallProjectionLeaseTTLEnv, defaultCodeCallProjectionLeaseTTL),
		BatchLimit:          loadPositiveIntOrDefault(getenv, codeCallProjectionBatchLimitEnv, defaultCodeCallProjectionBatchLimit),
		AcceptanceScanLimit: loadPositiveIntOrDefault(getenv, codeCallProjectionAcceptanceScanLimitEnv, defaultCodeCallProjectionAcceptanceScanLimit),
	}
}

func loadRepoDependencyProjectionConfig(getenv func(string) string) reducer.RepoDependencyProjectionRunnerConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	return reducer.RepoDependencyProjectionRunnerConfig{
		LeaseOwner:   loadStringOrDefault(getenv, repoDependencyProjectionLeaseOwnerEnv, defaultRepoDependencyProjectionLeaseOwner),
		PollInterval: loadDurationOrDefault(getenv, repoDependencyProjectionPollIntervalEnv, defaultRepoDependencyProjectionPollInterval),
		LeaseTTL:     loadDurationOrDefault(getenv, repoDependencyProjectionLeaseTTLEnv, defaultRepoDependencyProjectionLeaseTTL),
		BatchLimit:   loadPositiveIntOrDefault(getenv, repoDependencyProjectionBatchLimitEnv, defaultRepoDependencyProjectionBatchLimit),
	}
}

func loadCodeCallEdgeWriterTuning(getenv func(string) string) (int, int) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return loadPositiveIntOrDefault(getenv, codeCallEdgeBatchSizeEnv, defaultCodeCallEdgeBatchSize),
		loadPositiveIntOrDefault(getenv, codeCallEdgeGroupBatchSizeEnv, defaultCodeCallEdgeGroupBatchSize)
}

func loadSharedEdgeWriterGroupTuning(getenv func(string) string) (int, int) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return loadPositiveIntOrDefault(getenv, inheritanceEdgeGroupBatchSizeEnv, defaultInheritanceEdgeGroupBatchSize),
		loadPositiveIntOrDefault(getenv, sqlRelationshipEdgeGroupBatchSizeEnv, defaultSQLRelationshipEdgeGroupBatchSize)
}

func loadGraphProjectionPhaseRepairConfig(getenv func(string) string) reducer.GraphProjectionPhaseRepairerConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	return reducer.GraphProjectionPhaseRepairerConfig{
		PollInterval: loadDurationOrDefault(getenv, graphProjectionRepairPollIntervalEnv, defaultGraphProjectionRepairPollInterval),
		BatchLimit:   loadPositiveIntOrDefault(getenv, graphProjectionRepairBatchLimitEnv, defaultGraphProjectionRepairBatchLimit),
		RetryDelay:   loadDurationOrDefault(getenv, graphProjectionRepairRetryDelayEnv, defaultGraphProjectionRepairRetryDelay),
	}
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
