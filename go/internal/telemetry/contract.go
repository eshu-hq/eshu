// Package telemetry defines the frozen observability contract for the Go data
// plane. It intentionally contains only stable names and copy-safe accessors.
package telemetry

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

const (
	// DefaultServiceNamespace is the stable namespace shared by Go data-plane
	// runtimes when they publish telemetry resources.
	DefaultServiceNamespace = "eshu"

	// DefaultSignalName is the shared OTEL instrumentation name for the data
	// plane bootstrap substrate.
	DefaultSignalName = "eshu/go/data-plane"

	// InstrumentationScopeName is the stable package-level scope name used by
	// the telemetry bootstrap contract itself.
	InstrumentationScopeName = "eshu/go/internal/telemetry"
)

// Metric dimension keys define the stable labels used by the Go data-plane
// telemetry contract.
const (
	MetricDimensionScopeID       = "scope_id"
	MetricDimensionScopeKind     = "scope_kind"
	MetricDimensionSourceSystem  = "source_system"
	MetricDimensionGenerationID  = "generation_id"
	MetricDimensionCollectorKind = "collector_kind"
	MetricDimensionDomain        = "domain"
	MetricDimensionPartitionKey  = "partition_key"
	MetricDimensionRunner        = "runner"
	MetricDimensionLookupResult  = "lookup_result"
	MetricDimensionErrorType     = "error_type"
	MetricDimensionRepoSizeTier  = "repo_size_tier"
	MetricDimensionSkipReason    = "skip_reason"
	MetricDimensionNodeType      = "node_type"
	MetricDimensionEdgeType      = "edge_type"
	MetricDimensionWritePhase    = "write_phase"
	MetricDimensionOutcome       = "outcome"
)

// Span names define the stable data-plane tracing contract.
const (
	SpanCollectorObserve     = "collector.observe"
	SpanCollectorStream      = "collector.stream"
	SpanScopeAssign          = "scope.assign"
	SpanFactEmit             = "fact.emit"
	SpanProjectorRun         = "projector.run"
	SpanReducerIntentEnqueue = "reducer_intent.enqueue"
	SpanReducerRun           = "reducer.run"
	SpanReducerBatchClaim    = "reducer.batch_claim"
	SpanCanonicalWrite       = "canonical.write"
	SpanCanonicalProjection  = "canonical.projection"
	SpanCanonicalRetract     = "canonical.retract"

	SpanEvidenceDiscovery              = "ingestion.evidence_discovery"
	SpanIaCReachabilityMaterialization = "iac_reachability.materialize"
	SpanSQLRelationshipMaterialization = "reducer.sql_relationship_materialization"
	SpanInheritanceMaterialization     = "reducer.inheritance_materialization"
	SpanCrossRepoResolution            = "reducer.cross_repo_resolution"
	SpanSharedAcceptanceLookup         = "shared_acceptance.lookup"
	SpanSharedAcceptanceUpsert         = "shared_acceptance.upsert"
	SpanQueryRelationshipEvidence      = "query.relationship_evidence"
	SpanQueryDeadIaC                   = "query.dead_iac"
	SpanQueryInfraResourceSearch       = "query.infra_resource_search"

	// Dependency service spans — track external call performance.
	SpanPostgresExec  = "postgres.exec"
	SpanPostgresQuery = "postgres.query"
	SpanNeo4jExecute  = "neo4j.execute"
)

// Log keys define the structured logging contract for terminal failures and
// retryable failure classification.
const (
	LogKeyScopeID                = "scope_id"
	LogKeyScopeKind              = "scope_kind"
	LogKeySourceSystem           = "source_system"
	LogKeyGenerationID           = "generation_id"
	LogKeyCollectorKind          = "collector_kind"
	LogKeyDomain                 = "domain"
	LogKeyPartitionKey           = "partition_key"
	LogKeyRequestID              = "request_id"
	LogKeyFailureClass           = "failure_class"
	LogKeyRefreshSkipped         = "refresh_skipped"
	LogKeyPipelinePhase          = "pipeline_phase"
	LogKeyAcceptanceScopeID      = "acceptance.scope_id"
	LogKeyAcceptanceUnitID       = "acceptance.unit_id"
	LogKeyAcceptanceSourceRunID  = "acceptance.source_run_id"
	LogKeyAcceptanceGenerationID = "acceptance.generation_id"
	LogKeyAcceptanceStaleCount   = "acceptance.stale_count"
)

var metricDimensionKeys = []string{
	MetricDimensionScopeID,
	MetricDimensionScopeKind,
	MetricDimensionSourceSystem,
	MetricDimensionGenerationID,
	MetricDimensionCollectorKind,
	MetricDimensionDomain,
	MetricDimensionPartitionKey,
	MetricDimensionRunner,
	MetricDimensionLookupResult,
	MetricDimensionErrorType,
	MetricDimensionRepoSizeTier,
	MetricDimensionSkipReason,
	MetricDimensionNodeType,
	MetricDimensionEdgeType,
	MetricDimensionWritePhase,
	MetricDimensionOutcome,
}

var spanNames = []string{
	SpanCollectorObserve,
	SpanCollectorStream,
	SpanScopeAssign,
	SpanFactEmit,
	SpanProjectorRun,
	SpanReducerIntentEnqueue,
	SpanReducerRun,
	SpanReducerBatchClaim,
	SpanCanonicalWrite,
	SpanCanonicalProjection,
	SpanCanonicalRetract,
	SpanEvidenceDiscovery,
	SpanIaCReachabilityMaterialization,
	SpanSQLRelationshipMaterialization,
	SpanInheritanceMaterialization,
	SpanCrossRepoResolution,
	SpanSharedAcceptanceLookup,
	SpanSharedAcceptanceUpsert,
	SpanQueryRelationshipEvidence,
	SpanQueryDeadIaC,
	SpanQueryInfraResourceSearch,
	SpanPostgresExec,
	SpanPostgresQuery,
	SpanNeo4jExecute,
}

var logKeys = []string{
	LogKeyScopeID,
	LogKeyScopeKind,
	LogKeySourceSystem,
	LogKeyGenerationID,
	LogKeyCollectorKind,
	LogKeyDomain,
	LogKeyPartitionKey,
	LogKeyRequestID,
	LogKeyFailureClass,
	LogKeyRefreshSkipped,
	LogKeyPipelinePhase,
	LogKeyAcceptanceScopeID,
	LogKeyAcceptanceUnitID,
	LogKeyAcceptanceSourceRunID,
	LogKeyAcceptanceGenerationID,
	LogKeyAcceptanceStaleCount,
}

// MetricDimensionKeys returns the frozen ordered metric dimensions.
func MetricDimensionKeys() []string {
	return slices.Clone(metricDimensionKeys)
}

// SpanNames returns the frozen ordered span names.
func SpanNames() []string {
	return slices.Clone(spanNames)
}

// LogKeys returns the frozen ordered structured log keys.
func LogKeys() []string {
	return slices.Clone(logKeys)
}

// Bootstrap captures the minimum OpenTelemetry-first runtime settings needed
// by the Go data-plane bootstrap substrate.
type Bootstrap struct {
	ServiceName      string
	ServiceNamespace string
	MeterName        string
	TracerName       string
	LoggerName       string
}

// NewBootstrap constructs the stable telemetry bootstrap configuration for a
// service name.
func NewBootstrap(serviceName string) (Bootstrap, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return Bootstrap{}, errors.New("service name is required")
	}

	return Bootstrap{
		ServiceName:      serviceName,
		ServiceNamespace: DefaultServiceNamespace,
		MeterName:        DefaultSignalName,
		TracerName:       DefaultSignalName,
		LoggerName:       DefaultSignalName,
	}, nil
}

// Validate checks that the bootstrap contract is fully populated.
func (b Bootstrap) Validate() error {
	if strings.TrimSpace(b.ServiceName) == "" {
		return errors.New("service name is required")
	}
	if strings.TrimSpace(b.ServiceNamespace) == "" {
		return errors.New("service namespace is required")
	}
	if strings.TrimSpace(b.MeterName) == "" {
		return errors.New("meter name is required")
	}
	if strings.TrimSpace(b.TracerName) == "" {
		return errors.New("tracer name is required")
	}
	if strings.TrimSpace(b.LoggerName) == "" {
		return errors.New("logger name is required")
	}

	return nil
}

// ResourceAttributes returns the stable resource labels for the service.
func (b Bootstrap) ResourceAttributes() map[string]string {
	return maps.Clone(map[string]string{
		"service.name":      b.ServiceName,
		"service.namespace": b.ServiceNamespace,
	})
}

// InstrumentationScopeName returns the frozen scope name for the telemetry
// package bootstrap contract.
func (b Bootstrap) InstrumentationScopeName() string {
	return InstrumentationScopeName
}
