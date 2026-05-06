package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestCanonicalExecutorForGraphBackendWrapsNornicDBWithTimeout(t *testing.T) {
	t.Parallel()

	executor := canonicalExecutorForGraphBackend(
		contextBlockingIngesterExecutor{},
		runtimecfg.GraphBackendNornicDB,
		10*time.Millisecond,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		nil,
		nil,
	)

	err := executor.Execute(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() error = %v, want deadline exceeded", err)
	}
}

func TestNornicDBCanonicalGroupedWritesDefaultDisabled(t *testing.T) {
	t.Parallel()

	got, err := nornicDBCanonicalGroupedWrites(func(string) string { return "" })
	if err != nil {
		t.Fatalf("nornicDBCanonicalGroupedWrites() error = %v, want nil", err)
	}
	if got {
		t.Fatal("nornicDBCanonicalGroupedWrites() = true, want false by default")
	}
}

func TestNornicDBCanonicalGroupedWritesFromEnv(t *testing.T) {
	t.Parallel()

	got, err := nornicDBCanonicalGroupedWrites(func(key string) string {
		if key == "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES" {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("nornicDBCanonicalGroupedWrites() error = %v, want nil", err)
	}
	if !got {
		t.Fatal("nornicDBCanonicalGroupedWrites() = false, want true")
	}
}

func TestNornicDBCanonicalGroupedWritesRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := nornicDBCanonicalGroupedWrites(func(key string) string {
		if key == "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES" {
			return "maybe"
		}
		return ""
	})
	if err == nil {
		t.Fatal("nornicDBCanonicalGroupedWrites() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ESHU_NORNICDB_CANONICAL_GROUPED_WRITES") {
		t.Fatalf("nornicDBCanonicalGroupedWrites() error = %q, want env name", err)
	}
}

func TestNornicDBBatchedEntityContainmentDefaultDisabled(t *testing.T) {
	t.Parallel()

	got, err := nornicDBBatchedEntityContainmentEnabled(func(string) string { return "" })
	if err != nil {
		t.Fatalf("nornicDBBatchedEntityContainmentEnabled() error = %v, want nil", err)
	}
	if got {
		t.Fatal("nornicDBBatchedEntityContainmentEnabled() = true, want false by default")
	}
}

func TestNornicDBBatchedEntityContainmentFromEnv(t *testing.T) {
	t.Parallel()

	got, err := nornicDBBatchedEntityContainmentEnabled(func(key string) string {
		if key == nornicDBBatchedEntityContainmentEnv {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("nornicDBBatchedEntityContainmentEnabled() error = %v, want nil", err)
	}
	if !got {
		t.Fatal("nornicDBBatchedEntityContainmentEnabled() = false, want true")
	}
}

func TestNornicDBBatchedEntityContainmentRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := nornicDBBatchedEntityContainmentEnabled(func(key string) string {
		if key == nornicDBBatchedEntityContainmentEnv {
			return "sometimes"
		}
		return ""
	})
	if err == nil {
		t.Fatal("nornicDBBatchedEntityContainmentEnabled() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), nornicDBBatchedEntityContainmentEnv) {
		t.Fatalf("nornicDBBatchedEntityContainmentEnabled() error = %q, want env name", err)
	}
}

func TestNornicDBCanonicalWriteTimeoutDefault(t *testing.T) {
	t.Parallel()

	got := nornicDBCanonicalWriteTimeout(func(string) string { return "" })
	if got != defaultNornicDBCanonicalWriteTimeout {
		t.Fatalf("nornicDBCanonicalWriteTimeout() = %s, want %s", got, defaultNornicDBCanonicalWriteTimeout)
	}
}

func TestNornicDBCanonicalWriteTimeoutFromEnv(t *testing.T) {
	t.Parallel()

	got := nornicDBCanonicalWriteTimeout(func(key string) string {
		if key == canonicalWriteTimeoutEnv {
			return "2s"
		}
		return ""
	})
	if got != 2*time.Second {
		t.Fatalf("nornicDBCanonicalWriteTimeout() = %s, want 2s", got)
	}
}

func TestNornicDBPhaseGroupStatementsDefault(t *testing.T) {
	t.Parallel()

	got, err := nornicDBPhaseGroupStatements(func(string) string { return "" })
	if err != nil {
		t.Fatalf("nornicDBPhaseGroupStatements() error = %v, want nil", err)
	}
	if got != defaultNornicDBPhaseGroupStatements {
		t.Fatalf("nornicDBPhaseGroupStatements() = %d, want %d", got, defaultNornicDBPhaseGroupStatements)
	}
}

func TestNornicDBPhaseGroupStatementsFromEnv(t *testing.T) {
	t.Parallel()

	got, err := nornicDBPhaseGroupStatements(func(key string) string {
		if key == nornicDBPhaseGroupStatementsEnv {
			return "750"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("nornicDBPhaseGroupStatements() error = %v, want nil", err)
	}
	if got != 750 {
		t.Fatalf("nornicDBPhaseGroupStatements() = %d, want 750", got)
	}
}

func TestNornicDBPhaseGroupStatementsRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := nornicDBPhaseGroupStatements(func(key string) string {
		if key == nornicDBPhaseGroupStatementsEnv {
			return "nope"
		}
		return ""
	})
	if err == nil {
		t.Fatal("nornicDBPhaseGroupStatements() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), nornicDBPhaseGroupStatementsEnv) {
		t.Fatalf("nornicDBPhaseGroupStatements() error = %q, want env name", err)
	}
}

func TestNornicDBFilePhaseGroupStatementsDefault(t *testing.T) {
	t.Parallel()

	got, err := nornicDBFilePhaseGroupStatements(func(string) string { return "" })
	if err != nil {
		t.Fatalf("nornicDBFilePhaseGroupStatements() error = %v, want nil", err)
	}
	if got != defaultNornicDBFilePhaseStatements {
		t.Fatalf("nornicDBFilePhaseGroupStatements() = %d, want %d", got, defaultNornicDBFilePhaseStatements)
	}
}

func TestNornicDBFilePhaseGroupStatementsFromEnv(t *testing.T) {
	t.Parallel()

	got, err := nornicDBFilePhaseGroupStatements(func(key string) string {
		if key == nornicDBFilePhaseGroupStatementsEnv {
			return "7"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("nornicDBFilePhaseGroupStatements() error = %v, want nil", err)
	}
	if got != 7 {
		t.Fatalf("nornicDBFilePhaseGroupStatements() = %d, want 7", got)
	}
}

func TestNornicDBFilePhaseGroupStatementsRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := nornicDBFilePhaseGroupStatements(func(key string) string {
		if key == nornicDBFilePhaseGroupStatementsEnv {
			return "nope"
		}
		return ""
	})
	if err == nil {
		t.Fatal("nornicDBFilePhaseGroupStatements() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), nornicDBFilePhaseGroupStatementsEnv) {
		t.Fatalf("nornicDBFilePhaseGroupStatements() error = %q, want env name", err)
	}
}

func TestNornicDBFileBatchSizeDefault(t *testing.T) {
	t.Parallel()

	got, err := nornicDBFileBatchSize(func(string) string { return "" })
	if err != nil {
		t.Fatalf("nornicDBFileBatchSize() error = %v, want nil", err)
	}
	if got != defaultNornicDBFileBatchSize {
		t.Fatalf("nornicDBFileBatchSize() = %d, want %d", got, defaultNornicDBFileBatchSize)
	}
}

func TestNornicDBFileBatchSizeFromEnv(t *testing.T) {
	t.Parallel()

	got, err := nornicDBFileBatchSize(func(key string) string {
		if key == nornicDBFileBatchSizeEnv {
			return "75"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("nornicDBFileBatchSize() error = %v, want nil", err)
	}
	if got != 75 {
		t.Fatalf("nornicDBFileBatchSize() = %d, want 75", got)
	}
}

func TestNornicDBFileBatchSizeRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := nornicDBFileBatchSize(func(key string) string {
		if key == nornicDBFileBatchSizeEnv {
			return "nope"
		}
		return ""
	})
	if err == nil {
		t.Fatal("nornicDBFileBatchSize() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), nornicDBFileBatchSizeEnv) {
		t.Fatalf("nornicDBFileBatchSize() error = %q, want env name", err)
	}
}

func TestNornicDBEntityPhaseGroupStatementsDefault(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityPhaseGroupStatements(func(string) string { return "" })
	if err != nil {
		t.Fatalf("nornicDBEntityPhaseGroupStatements() error = %v, want nil", err)
	}
	if got != defaultNornicDBEntityPhaseStatements {
		t.Fatalf("nornicDBEntityPhaseGroupStatements() = %d, want %d", got, defaultNornicDBEntityPhaseStatements)
	}
}

func TestNornicDBEntityPhaseGroupStatementsFromEnv(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityPhaseGroupStatements(func(key string) string {
		if key == nornicDBEntityPhaseStatementsEnv {
			return "33"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("nornicDBEntityPhaseGroupStatements() error = %v, want nil", err)
	}
	if got != 33 {
		t.Fatalf("nornicDBEntityPhaseGroupStatements() = %d, want 33", got)
	}
}

func TestNornicDBEntityPhaseGroupStatementsRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := nornicDBEntityPhaseGroupStatements(func(key string) string {
		if key == nornicDBEntityPhaseStatementsEnv {
			return "nope"
		}
		return ""
	})
	if err == nil {
		t.Fatal("nornicDBEntityPhaseGroupStatements() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), nornicDBEntityPhaseStatementsEnv) {
		t.Fatalf("nornicDBEntityPhaseGroupStatements() error = %q, want env name", err)
	}
}

func TestNornicDBEntityBatchSizeDefault(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityBatchSize(func(string) string { return "" })
	if err != nil {
		t.Fatalf("nornicDBEntityBatchSize() error = %v, want nil", err)
	}
	if got != defaultNornicDBEntityBatchSize {
		t.Fatalf("nornicDBEntityBatchSize() = %d, want %d", got, defaultNornicDBEntityBatchSize)
	}
}

func TestNornicDBEntityBatchSizeFromEnv(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityBatchSize(func(key string) string {
		if key == nornicDBEntityBatchSizeEnv {
			return "100"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("nornicDBEntityBatchSize() error = %v, want nil", err)
	}
	if got != 100 {
		t.Fatalf("nornicDBEntityBatchSize() = %d, want 100", got)
	}
}

func TestNornicDBEntityBatchSizeRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := nornicDBEntityBatchSize(func(key string) string {
		if key == nornicDBEntityBatchSizeEnv {
			return "nope"
		}
		return ""
	})
	if err == nil {
		t.Fatal("nornicDBEntityBatchSize() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), nornicDBEntityBatchSizeEnv) {
		t.Fatalf("nornicDBEntityBatchSize() error = %q, want env name", err)
	}
}
