// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package log provides canonical slog attribute constructors for the Eshu data
// plane. Every attribute function returns a slog.Attr with a stable key name.
// Keys that overlap with the frozen telemetry contract reference
// go/internal/telemetry constants directly so a key rename in the contract
// automatically propagates to all log sites.
//
// Usage:
//
//	logger.ErrorContext(ctx, "msg", log.Err(err), log.CollectorKind(k))
//	logger.InfoContext(ctx, "msg", log.ScopeID(sid), log.Domain(d))
package log

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// --- telemetry-backed attribute constructors ---

// ScopeID returns a scope_id log attribute backed by the frozen telemetry contract.
func ScopeID(id string) slog.Attr { return slog.String(telemetry.LogKeyScopeID, id) }

// ScopeKind returns a scope_kind log attribute backed by the frozen telemetry contract.
func ScopeKind(k string) slog.Attr { return slog.String(telemetry.LogKeyScopeKind, k) }

// CollectorKind returns a collector_kind log attribute backed by the frozen telemetry contract.
func CollectorKind(k string) slog.Attr { return slog.String(telemetry.LogKeyCollectorKind, k) }

// Domain returns a domain log attribute backed by the frozen telemetry contract.
func Domain(d string) slog.Attr { return slog.String(telemetry.LogKeyDomain, d) }

// GenerationID returns a generation_id log attribute backed by the frozen telemetry contract.
func GenerationID(id string) slog.Attr { return slog.String(telemetry.LogKeyGenerationID, id) }

// FailureClass returns a failure_class log attribute backed by the frozen telemetry contract.
func FailureClass(c string) slog.Attr { return slog.String(telemetry.LogKeyFailureClass, c) }

// PipelinePhase returns a pipeline_phase log attribute backed by the frozen telemetry contract.
func PipelinePhase(p string) slog.Attr { return slog.String(telemetry.LogKeyPipelinePhase, p) }

// RequestID returns a request_id log attribute backed by the frozen telemetry contract.
func RequestID(id string) slog.Attr { return slog.String(telemetry.LogKeyRequestID, id) }

// SourceSystem returns a source_system log attribute backed by the frozen telemetry contract.
func SourceSystem(s string) slog.Attr { return slog.String(telemetry.LogKeySourceSystem, s) }

// PartitionKey returns a partition_key log attribute backed by the frozen telemetry contract.
func PartitionKey(k string) slog.Attr { return slog.String(telemetry.LogKeyPartitionKey, k) }

// --- pkg/log-owned attribute key constants ---

const (
	// KeyError is the canonical log key for error messages. It shares the "error"
	// wire key with telemetry.LogKeyDriftCompositeError by intent.
	KeyError = "error"

	// KeyTenantID is the canonical log key for tenant identifiers.
	KeyTenantID = "tenant_id"

	// KeyRepoPath is the canonical log key for repository paths.  High-cardinality;
	// must never appear in metric labels.  Suitable only for structured logs and
	// span attributes.
	KeyRepoPath = "repo_path"

	// KeyQueue is the canonical log key for queue names.
	KeyQueue = "queue"

	// KeyIntentID is the canonical log key for reducer intent identifiers.
	// High-cardinality; must never appear in metric labels.
	KeyIntentID = "intent_id"

	// KeyWorkerID is the canonical log key for worker identifiers.
	KeyWorkerID = "worker_id"

	// KeyComponent is the canonical log key for runtime component names.
	KeyComponent = "component"

	// KeyRuntimeRole is the canonical log key for the binary's runtime role
	// (e.g. "api", "reducer", "ingester").
	KeyRuntimeRole = "runtime_role"

	// KeyRepositoryID is the canonical log key for internal repository identifiers.
	// High-cardinality; must never appear in metric labels.
	KeyRepositoryID = "repository_id"

	// KeyWorkloadID is the canonical log key for workload/deployable-unit identifiers.
	// High-cardinality; must never appear in metric labels.
	KeyWorkloadID = "workload_id"

	// KeyClusterID is the canonical log key for Kubernetes cluster identifiers.
	KeyClusterID = "cluster_id"

	// KeyElapsedSeconds is the canonical log key for elapsed-time measurements.
	KeyElapsedSeconds = "elapsed_seconds"

	// KeyBatchSize is the canonical log key for batch sizes.
	KeyBatchSize = "batch_size"

	// KeySkipReason is the canonical log key for skip reasons.
	KeySkipReason = "skip_reason"

	// KeyLanguage is the canonical log key for programming language identifiers.
	KeyLanguage = "language"

	// KeyProvider is the canonical log key for cloud/infra provider names.  Wire
	// value intentionally matches telemetry.MetricDimensionProvider so operators
	// can correlate log lines and metric labels on the same key.
	KeyProvider = "provider"

	// KeyOperation is the canonical log key for operation names.  Wire value
	// intentionally matches telemetry.MetricDimensionOperation.
	KeyOperation = "operation"

	// KeyStatus is the canonical log key for status values.  Wire value
	// intentionally matches telemetry.MetricDimensionStatus.
	KeyStatus = "status"

	// KeyEventKind is the canonical log key for event classification.  Wire value
	// intentionally matches telemetry.MetricDimensionEventKind.
	KeyEventKind = "event_kind"

	// KeyEventName is the canonical log key for named event identifiers.
	KeyEventName = "event_name"
)

// --- pkg/log-owned attribute constructors ---

// Err returns an error log attribute from an error value.  A nil error
// produces an empty attribute so callers can unconditionally pass err without
// guarding for nil.
func Err(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	return slog.String(KeyError, err.Error())
}

// ErrStr returns an error log attribute from a string.
func ErrStr(msg string) slog.Attr { return slog.String(KeyError, msg) }

// TenantID returns a tenant_id log attribute.
func TenantID(id string) slog.Attr { return slog.String(KeyTenantID, id) }

// RepoPath returns a repo_path log attribute.
func RepoPath(p string) slog.Attr { return slog.String(KeyRepoPath, p) }

// Queue returns a queue log attribute.
func Queue(q string) slog.Attr { return slog.String(KeyQueue, q) }

// IntentID returns an intent_id log attribute.
func IntentID(id string) slog.Attr { return slog.String(KeyIntentID, id) }

// WorkerID returns a worker_id log attribute.
func WorkerID(id string) slog.Attr { return slog.String(KeyWorkerID, id) }

// Component returns a component log attribute.
func Component(c string) slog.Attr { return slog.String(KeyComponent, c) }

// RuntimeRole returns a runtime_role log attribute.
func RuntimeRole(r string) slog.Attr { return slog.String(KeyRuntimeRole, r) }

// RepositoryID returns a repository_id log attribute.
func RepositoryID(id string) slog.Attr { return slog.String(KeyRepositoryID, id) }

// WorkloadID returns a workload_id log attribute.
func WorkloadID(id string) slog.Attr { return slog.String(KeyWorkloadID, id) }

// ClusterID returns a cluster_id log attribute.
func ClusterID(id string) slog.Attr { return slog.String(KeyClusterID, id) }

// ElapsedSeconds returns an elapsed_seconds log attribute.
func ElapsedSeconds(v float64) slog.Attr { return slog.Float64(KeyElapsedSeconds, v) }

// BatchSize returns a batch_size log attribute.
func BatchSize(n int) slog.Attr { return slog.Int(KeyBatchSize, n) }

// SkipReason returns a skip_reason log attribute.
func SkipReason(r string) slog.Attr { return slog.String(KeySkipReason, r) }

// Language returns a language log attribute.
func Language(l string) slog.Attr { return slog.String(KeyLanguage, l) }

// Provider returns a provider log attribute.
func Provider(p string) slog.Attr { return slog.String(KeyProvider, p) }

// Operation returns an operation log attribute.
func Operation(o string) slog.Attr { return slog.String(KeyOperation, o) }

// Status returns a status log attribute.
func Status(s string) slog.Attr { return slog.String(KeyStatus, s) }

// EventKind returns an event_kind log attribute.
func EventKind(k string) slog.Attr { return slog.String(KeyEventKind, k) }

// EventName returns an event_name log attribute.
func EventName(n string) slog.Attr { return slog.String(KeyEventName, n) }

// --- context-aware With* helpers matching the Epic U convention ---

// WithTenant returns a tenant_id attribute for structured logging.
func WithTenant(_ context.Context, id string) slog.Attr { return slog.String(KeyTenantID, id) }

// WithCollectorKind returns a collector_kind attribute for structured logging.
func WithCollectorKind(_ context.Context, k string) slog.Attr {
	return slog.String(telemetry.LogKeyCollectorKind, k)
}

// WithScopeID returns a scope_id attribute for structured logging.
func WithScopeID(_ context.Context, id string) slog.Attr {
	return slog.String(telemetry.LogKeyScopeID, id)
}

// WithDomain returns a domain attribute for structured logging.
func WithDomain(_ context.Context, d string) slog.Attr {
	return slog.String(telemetry.LogKeyDomain, d)
}

// WithGenerationID returns a generation_id attribute for structured logging.
func WithGenerationID(_ context.Context, id string) slog.Attr {
	return slog.String(telemetry.LogKeyGenerationID, id)
}

// WithFailureClass returns a failure_class attribute for structured logging.
func WithFailureClass(_ context.Context, c string) slog.Attr {
	return slog.String(telemetry.LogKeyFailureClass, c)
}

// WithPipelinePhase returns a pipeline_phase attribute for structured logging.
func WithPipelinePhase(_ context.Context, p string) slog.Attr {
	return slog.String(telemetry.LogKeyPipelinePhase, p)
}
