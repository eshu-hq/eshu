// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

var repoDependencyProjectionBootNonce = newRepoDependencyProjectionBootNonce()

const (
	codeCallProjectionPollIntervalEnv        = "ESHU_CODE_CALL_PROJECTION_POLL_INTERVAL"
	codeCallProjectionLeaseTTLEnv            = "ESHU_CODE_CALL_PROJECTION_LEASE_TTL"
	codeCallProjectionBatchLimitEnv          = "ESHU_CODE_CALL_PROJECTION_BATCH_LIMIT"
	codeCallProjectionAcceptanceScanLimitEnv = "ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT"
	codeCallProjectionLeaseOwnerEnv          = "ESHU_CODE_CALL_PROJECTION_LEASE_OWNER"
	codeCallProjectionPartitionCountEnv      = "ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT"
	codeCallProjectionWorkersEnv             = "ESHU_CODE_CALL_PROJECTION_WORKERS"
	repoDependencyProjectionPollIntervalEnv  = "ESHU_REPO_DEPENDENCY_PROJECTION_POLL_INTERVAL"
	repoDependencyProjectionLeaseTTLEnv      = "ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_TTL"
	repoDependencyProjectionCycleTimeoutEnv  = "ESHU_REPO_DEPENDENCY_PROJECTION_CYCLE_TIMEOUT"
	repoDependencyProjectionBatchLimitEnv    = "ESHU_REPO_DEPENDENCY_PROJECTION_BATCH_LIMIT"
	repoDependencyProjectionLeaseOwnerEnv    = "ESHU_REPO_DEPENDENCY_PROJECTION_LEASE_OWNER"
	repoDependencyProjectionWorkersEnv       = "ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS"
	repoDependencyRetractStatementTimingEnv  = "ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING"
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
	defaultCodeCallProjectionPartitionCount      = 8
	defaultCodeCallProjectionWorkers             = 4
	defaultRepoDependencyProjectionPollInterval  = 500 * time.Millisecond
	defaultRepoDependencyProjectionLeaseTTL      = 5 * time.Minute
	defaultRepoDependencyProjectionCycleTimeout  = 45 * time.Second
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
		PartitionCount:      loadPositiveIntOrDefault(getenv, codeCallProjectionPartitionCountEnv, defaultCodeCallProjectionPartitionCount),
		Workers:             loadPositiveIntOrDefault(getenv, codeCallProjectionWorkersEnv, defaultCodeCallProjectionWorkers),
	}
}

func loadRepoDependencyProjectionConfig(getenv func(string) string) reducer.RepoDependencyProjectionRunnerConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	return reducer.RepoDependencyProjectionRunnerConfig{
		LeaseOwner:            loadRepoDependencyProjectionLeaseOwner(getenv),
		PollInterval:          loadDurationOrDefault(getenv, repoDependencyProjectionPollIntervalEnv, defaultRepoDependencyProjectionPollInterval),
		LeaseTTL:              loadDurationOrDefault(getenv, repoDependencyProjectionLeaseTTLEnv, defaultRepoDependencyProjectionLeaseTTL),
		CycleTimeout:          loadDurationOrDefault(getenv, repoDependencyProjectionCycleTimeoutEnv, defaultRepoDependencyProjectionCycleTimeout),
		GraphQuiescenceBudget: nornicDBCanonicalWriteTimeout(getenv),
		BatchLimit:            loadPositiveIntOrDefault(getenv, repoDependencyProjectionBatchLimitEnv, defaultRepoDependencyProjectionBatchLimit),
		Workers:               loadRepoDependencyProjectionWorkers(getenv),
	}
}

func loadRepoDependencyProjectionWorkers(getenv func(string) string) int {
	switch strings.TrimSpace(getenv(repoDependencyProjectionWorkersEnv)) {
	case "2":
		return 2
	case "4":
		return 4
	case "1", "":
		return 1
	default:
		return 1
	}
}

func loadRepoDependencyProjectionLeaseOwner(getenv func(string) string) string {
	prefix := loadStringOrDefault(
		getenv,
		repoDependencyProjectionLeaseOwnerEnv,
		defaultRepoDependencyProjectionLeaseOwner,
	)
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s:%s:%d:%s", prefix, hostname, os.Getpid(), repoDependencyProjectionBootNonce)
}

func newRepoDependencyProjectionBootNonce() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return fmt.Sprintf("%016x", time.Now().UnixNano())
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
