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
	MetricDimensionSource        = "source"
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
	MetricDimensionBackendKind   = "backend_kind"
	MetricDimensionResult        = "result"
	MetricDimensionReason        = "reason"
	// MetricDimensionSafeLocatorHash labels Terraform-state metrics with the
	// scope-level safe locator hash so operators can group counters per state
	// without exposing bucket names, S3 keys, or local paths.
	MetricDimensionSafeLocatorHash = "safe_locator_hash"
	// MetricDimensionWarningKind labels Terraform-state warning metrics with
	// the warning category (state_in_vcs, state_too_large, output_value_dropped,
	// etc.) emitted by the streaming parser.
	MetricDimensionWarningKind = "warning_kind"
	// MetricDimensionPack labels correlation metrics with the rule-pack
	// identifier (e.g. "terraform_config_state_drift"). The value space is the
	// fixed set of pack names registered through FirstPartyRulePacks().
	MetricDimensionPack = "pack"
	// MetricDimensionRule labels correlation metrics with one rule name from
	// the rule pack (e.g. "match-config-against-state"). Cardinality is
	// bounded by the rule count in each pack.
	MetricDimensionRule = "rule"
	// MetricDimensionDriftKind labels drift-detection metrics with the closed
	// enum of drift kinds: added_in_state, added_in_config, attribute_drift,
	// removed_from_state, removed_from_config.
	MetricDimensionDriftKind = "drift_kind"
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
	// SpanReducerDriftEvidenceLoad wraps the PostgresDriftEvidenceLoader call
	// for one config_state_drift intent. Children spans on the InstrumentedDB
	// surface each component query (config-side terraform_resources, active
	// state-snapshot lookup, current and optional prior state-resources, and
	// the prior-config-addresses walk that activates removed_from_config).
	// Operators reading a trace can tell which of those is slow without
	// instrumenting each call site individually.
	SpanReducerDriftEvidenceLoad = "reducer.drift_evidence_load"
	SpanCanonicalWrite       = "canonical.write"
	SpanCanonicalProjection  = "canonical.projection"
	SpanCanonicalRetract     = "canonical.retract"

	SpanEvidenceDiscovery                 = "ingestion.evidence_discovery"
	SpanIaCReachabilityMaterialization    = "iac_reachability.materialize"
	SpanSQLRelationshipMaterialization    = "reducer.sql_relationship_materialization"
	SpanInheritanceMaterialization        = "reducer.inheritance_materialization"
	SpanCrossRepoResolution               = "reducer.cross_repo_resolution"
	SpanSharedAcceptanceLookup            = "shared_acceptance.lookup"
	SpanSharedAcceptanceUpsert            = "shared_acceptance.upsert"
	SpanQueryRelationshipEvidence         = "query.relationship_evidence"
	SpanQueryDocumentationFindings        = "query.documentation_findings"
	SpanQueryDocumentationEvidencePacket  = "query.documentation_evidence_packet"
	SpanQueryDocumentationPacketFreshness = "query.documentation_packet_freshness"
	SpanQueryDeadIaC                      = "query.dead_iac"
	SpanQueryInfraResourceSearch          = "query.infra_resource_search"
	SpanTerraformStateClaimProcess        = "tfstate.collector.claim.process"
	SpanTerraformStateDiscoveryResolve    = "tfstate.discovery.resolve"
	SpanTerraformStateSourceOpen          = "tfstate.source.open"
	SpanTerraformStateParserStream        = "tfstate.parser.stream"
	SpanTerraformStateFactEmitBatch       = "tfstate.fact.emit_batch"
	SpanTerraformStateCoordinatorDone     = "tfstate.coordinator.complete"

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

	// LogKeyDriftPriorConfigDepth is the effective prior-config-walk depth
	// bound applied when scanning prior repo-snapshot generations for
	// removed_from_config evidence. Emitted by the drift evidence loader so
	// operators can confirm the ESHU_DRIFT_PRIOR_CONFIG_DEPTH knob took effect.
	LogKeyDriftPriorConfigDepth = "depth"
	// LogKeyDriftPriorConfigAddresses is the count of unique resource addresses
	// found across all prior-config-snapshot generations within the depth window.
	// A lower-than-expected count here is the first signal that the window is
	// too narrow to catch a removal.
	LogKeyDriftPriorConfigAddresses = "prior_config_addresses"
	// LogKeyDriftStateOnlyAddresses is the count of addresses present in state
	// but absent from the current config snapshot. Includes both
	// removed_from_config candidates (promoted) and plain added_in_state
	// addresses (outside the depth window).
	LogKeyDriftStateOnlyAddresses = "state_only_addresses"
	// LogKeyDriftAddressesPromoted is the count of state-only addresses that
	// were found in the prior-config set and promoted to
	// PreviouslyDeclaredInConfig=true, enabling the classifier to emit
	// removed_from_config. The wire key intentionally matches the classifier
	// kind label so log lines and metric labels share the same terminology.
	LogKeyDriftAddressesPromoted = "addresses_promoted_to_removed_from_config"
)

var metricDimensionKeys = []string{
	MetricDimensionScopeID,
	MetricDimensionScopeKind,
	MetricDimensionSource,
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
	MetricDimensionBackendKind,
	MetricDimensionResult,
	MetricDimensionReason,
	MetricDimensionSafeLocatorHash,
	MetricDimensionWarningKind,
	MetricDimensionPack,
	MetricDimensionRule,
	MetricDimensionDriftKind,
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
	SpanQueryDocumentationFindings,
	SpanQueryDocumentationEvidencePacket,
	SpanQueryDocumentationPacketFreshness,
	SpanQueryDeadIaC,
	SpanQueryInfraResourceSearch,
	SpanTerraformStateClaimProcess,
	SpanTerraformStateDiscoveryResolve,
	SpanTerraformStateSourceOpen,
	SpanTerraformStateParserStream,
	SpanTerraformStateFactEmitBatch,
	SpanTerraformStateCoordinatorDone,
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
	LogKeyDriftPriorConfigDepth,
	LogKeyDriftPriorConfigAddresses,
	LogKeyDriftStateOnlyAddresses,
	LogKeyDriftAddressesPromoted,
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
