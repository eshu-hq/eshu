package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func ingesterContentBeforeCanonical(getenv func(string) string) bool {
	return strings.TrimSpace(getenv("ESHU_QUERY_PROFILE")) == "local_authoritative"
}

func nornicDBCanonicalWriteTimeout(getenv func(string) string) time.Duration {
	raw := strings.TrimSpace(getenv(canonicalWriteTimeoutEnv))
	if raw == "" {
		return defaultNornicDBCanonicalWriteTimeout
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return defaultNornicDBCanonicalWriteTimeout
	}
	return parsed
}

func nornicDBCanonicalGroupedWrites(getenv func(string) string) (bool, error) {
	raw := strings.TrimSpace(getenv(nornicDBCanonicalGroupedWritesEnv))
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s=%q: %w", nornicDBCanonicalGroupedWritesEnv, raw, err)
	}
	return enabled, nil
}

func nornicDBBatchedEntityContainmentEnabled(getenv func(string) string) (bool, error) {
	raw := strings.TrimSpace(getenv(nornicDBBatchedEntityContainmentEnv))
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s=%q: %w", nornicDBBatchedEntityContainmentEnv, raw, err)
	}
	return enabled, nil
}

func nornicDBPhaseGroupStatements(getenv func(string) string) (int, error) {
	raw := strings.TrimSpace(getenv(nornicDBPhaseGroupStatementsEnv))
	if raw == "" {
		return defaultNornicDBPhaseGroupStatements, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("parse %s=%q: must be a positive integer", nornicDBPhaseGroupStatementsEnv, raw)
	}
	return n, nil
}

func nornicDBFilePhaseGroupStatements(getenv func(string) string) (int, error) {
	raw := strings.TrimSpace(getenv(nornicDBFilePhaseGroupStatementsEnv))
	if raw == "" {
		return defaultNornicDBFilePhaseStatements, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("parse %s=%q: must be a positive integer", nornicDBFilePhaseGroupStatementsEnv, raw)
	}
	return n, nil
}

func nornicDBFileBatchSize(getenv func(string) string) (int, error) {
	raw := strings.TrimSpace(getenv(nornicDBFileBatchSizeEnv))
	if raw == "" {
		return defaultNornicDBFileBatchSize, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("parse %s=%q: must be a positive integer", nornicDBFileBatchSizeEnv, raw)
	}
	return n, nil
}

func nornicDBEntityPhaseGroupStatements(getenv func(string) string) (int, error) {
	raw := strings.TrimSpace(getenv(nornicDBEntityPhaseStatementsEnv))
	if raw == "" {
		return defaultNornicDBEntityPhaseStatements, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("parse %s=%q: must be a positive integer", nornicDBEntityPhaseStatementsEnv, raw)
	}
	return n, nil
}

func nornicDBEntityBatchSize(getenv func(string) string) (int, error) {
	raw := strings.TrimSpace(getenv(nornicDBEntityBatchSizeEnv))
	if raw == "" {
		return defaultNornicDBEntityBatchSize, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("parse %s=%q: must be a positive integer", nornicDBEntityBatchSizeEnv, raw)
	}
	return n, nil
}

func neo4jBatchSize(getenv func(string) string) int {
	raw := strings.TrimSpace(getenv("ESHU_NEO4J_BATCH_SIZE"))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
