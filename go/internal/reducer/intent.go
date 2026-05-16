// Package reducer defines the durable cross-source and cross-scope reducer
// substrate used by the Go data plane.
package reducer

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Domain identifies a canonical shared-truth reducer domain.
type Domain string

const (
	// DomainWorkloadIdentity resolves canonical workload identity.
	DomainWorkloadIdentity Domain = "workload_identity"
	// DomainDeployableUnitCorrelation correlates cross-source deployable-unit
	// evidence before workload admission and materialization.
	DomainDeployableUnitCorrelation Domain = "deployable_unit_correlation"
	// DomainCloudAssetResolution resolves canonical cloud asset identity.
	DomainCloudAssetResolution Domain = "cloud_asset_resolution"
	// DomainDeploymentMapping resolves deployment relationships.
	DomainDeploymentMapping Domain = "deployment_mapping"
	// DomainDataLineage resolves lineage across sources and scopes.
	DomainDataLineage Domain = "data_lineage"
	// DomainOwnership resolves ownership and responsibility records.
	DomainOwnership Domain = "ownership"
	// DomainGovernance resolves governance and policy attribution.
	DomainGovernance Domain = "governance"
	// DomainWorkloadMaterialization materializes canonical workload graph nodes.
	DomainWorkloadMaterialization Domain = "workload_materialization"
	// DomainCodeCallMaterialization materializes canonical code call edges.
	DomainCodeCallMaterialization Domain = "code_call_materialization"
	// DomainSemanticEntityMaterialization materializes Annotation, Typedef,
	// TypeAlias, and Component semantic nodes.
	DomainSemanticEntityMaterialization Domain = "semantic_entity_materialization"
	// DomainSQLRelationshipMaterialization materializes canonical SQL
	// relationship edges (REFERENCES_TABLE, HAS_COLUMN, TRIGGERS).
	DomainSQLRelationshipMaterialization Domain = "sql_relationship_materialization"
	// DomainInheritanceMaterialization materializes canonical inheritance,
	// override, and alias edges from parser entity bases and trait adaptation
	// metadata.
	DomainInheritanceMaterialization Domain = "inheritance_materialization"
	// DomainConfigStateDrift correlates Terraform config (parsed HCL) against
	// Terraform state to detect five drift kinds. Cross-source, cross-scope,
	// non-canonical-write — counters and structured logs are the v1 surface.
	// Design contract:
	// docs/superpowers/plans/2026-05-10-tfstate-config-state-drift-design.md
	DomainConfigStateDrift Domain = "config_state_drift"
	// DomainPackageSourceCorrelation classifies package-registry source hints
	// against active repository remotes without promoting package ownership.
	DomainPackageSourceCorrelation Domain = "package_source_correlation"
	// DomainContainerImageIdentity joins Git, registry, and runtime image
	// evidence into digest-keyed container image identity decisions.
	DomainContainerImageIdentity Domain = "container_image_identity"
	// DomainCICDRunCorrelation correlates provider CI/CD runs, artifacts, and
	// environment observations with reducer-owned artifact identity evidence.
	DomainCICDRunCorrelation Domain = "ci_cd_run_correlation"
	// DomainSBOMAttestationAttachment attaches SBOM and attestation evidence to
	// image digests only when subject evidence is explicit.
	DomainSBOMAttestationAttachment Domain = "sbom_attestation_attachment"
	// DomainAWSCloudRuntimeDrift publishes admitted AWS runtime-vs-IaC drift
	// findings as canonical reducer facts. The domain stays graph-neutral until
	// the drift node and query shape are frozen.
	DomainAWSCloudRuntimeDrift Domain = "aws_cloud_runtime_drift"
)

// IntentStatus captures the durable reducer intent lifecycle state.
type IntentStatus string

const (
	// IntentStatusPending means the intent is ready to be claimed.
	IntentStatusPending IntentStatus = "pending"
	// IntentStatusClaimed means the intent has been leased for execution.
	IntentStatusClaimed IntentStatus = "claimed"
	// IntentStatusRunning means the reducer is actively processing the intent.
	IntentStatusRunning IntentStatus = "running"
	// IntentStatusSucceeded means the intent finished successfully.
	IntentStatusSucceeded IntentStatus = "succeeded"
	// IntentStatusFailed means the intent is terminally failed.
	IntentStatusFailed IntentStatus = "failed"
)

// ResultStatus captures the terminal outcome of one reducer execution.
type ResultStatus string

const (
	// ResultStatusSucceeded means the execution completed successfully.
	ResultStatusSucceeded ResultStatus = "succeeded"
	// ResultStatusFailed means the execution failed.
	ResultStatusFailed ResultStatus = "failed"
	// ResultStatusSuperseded means the intent was skipped because a newer
	// generation is already active for the scope.
	ResultStatusSuperseded ResultStatus = "superseded"
)

// FailureRecord captures the durable reducer failure classification.
type FailureRecord struct {
	FailureClass string
	Message      string
	Details      string
}

// RetryableError marks reducer failures that should re-enter the durable
// queue instead of becoming terminal on the first failure.
type RetryableError interface {
	error
	Retryable() bool
}

// IsRetryable reports whether the supplied error explicitly opts into bounded
// retry behavior.
func IsRetryable(err error) bool {
	var retryable RetryableError
	if !errors.As(err, &retryable) {
		return false
	}

	return retryable.Retryable()
}

// Intent describes one durable reducer follow-up action keyed by scope
// generation.
type Intent struct {
	IntentID        string
	ScopeID         string
	GenerationID    string
	SourceSystem    string
	Domain          Domain
	Cause           string
	Priority        int
	AttemptCount    int
	EntityKeys      []string
	RelatedScopeIDs []string
	Status          IntentStatus
	EnqueuedAt      time.Time
	AvailableAt     time.Time
	ClaimedAt       *time.Time
	CompletedAt     *time.Time
	Failure         *FailureRecord
}

// ScopeGenerationKey returns the durable scope-generation boundary for the intent.
func (i Intent) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", i.ScopeID, i.GenerationID)
}

// Validate checks the durable intent contract.
func (i Intent) Validate() error {
	if strings.TrimSpace(i.IntentID) == "" {
		return errors.New("intent_id must not be blank")
	}
	if strings.TrimSpace(i.ScopeID) == "" {
		return errors.New("scope_id must not be blank")
	}
	if strings.TrimSpace(i.GenerationID) == "" {
		return errors.New("generation_id must not be blank")
	}
	if strings.TrimSpace(i.SourceSystem) == "" {
		return errors.New("source_system must not be blank")
	}
	if err := i.Domain.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(i.Cause) == "" {
		return errors.New("cause must not be blank")
	}
	if i.EnqueuedAt.IsZero() {
		return errors.New("enqueued_at must not be zero")
	}
	if i.AvailableAt.IsZero() {
		return errors.New("available_at must not be zero")
	}
	if len(i.RelatedScopeIDs) == 0 {
		return errors.New("related_scope_ids must not be empty")
	}
	if err := i.Status.Validate(); err != nil {
		return err
	}

	for _, key := range i.EntityKeys {
		if strings.TrimSpace(key) == "" {
			return errors.New("entity_keys must not contain blank values")
		}
	}
	var seenRelatedScopes map[string]struct{}
	for _, scopeID := range i.RelatedScopeIDs {
		normalizedScopeID := strings.TrimSpace(scopeID)
		if normalizedScopeID == "" {
			return errors.New("related_scope_ids must not contain blank values")
		}
		if seenRelatedScopes == nil {
			seenRelatedScopes = make(map[string]struct{}, len(i.RelatedScopeIDs))
		}
		if _, exists := seenRelatedScopes[normalizedScopeID]; exists {
			return errors.New("related_scope_ids must not contain duplicate values")
		}
		seenRelatedScopes[normalizedScopeID] = struct{}{}
	}

	return nil
}

// Clone returns a replay-safe copy of the intent.
func (i Intent) Clone() Intent {
	cloned := i
	cloned.EntityKeys = slices.Clone(i.EntityKeys)
	cloned.RelatedScopeIDs = slices.Clone(i.RelatedScopeIDs)
	if i.ClaimedAt != nil {
		claimedAt := *i.ClaimedAt
		cloned.ClaimedAt = &claimedAt
	}
	if i.CompletedAt != nil {
		completedAt := *i.CompletedAt
		cloned.CompletedAt = &completedAt
	}
	if i.Failure != nil {
		failure := *i.Failure
		cloned.Failure = &failure
	}

	return cloned
}

// Validate checks that the lifecycle state is one of the known durable values.
func (status IntentStatus) Validate() error {
	switch status {
	case IntentStatusPending, IntentStatusClaimed, IntentStatusRunning, IntentStatusSucceeded, IntentStatusFailed:
		return nil
	default:
		return fmt.Errorf("unknown intent status %q", status)
	}
}

// Terminal reports whether the status represents a final state.
func (status IntentStatus) Terminal() bool {
	switch status {
	case IntentStatusSucceeded, IntentStatusFailed:
		return true
	default:
		return false
	}
}

// WithStatus returns a clone of the intent with the given status and timestamp.
func (i Intent) WithStatus(status IntentStatus, at time.Time) Intent {
	cloned := i.Clone()
	cloned.Status = status
	switch status {
	case IntentStatusClaimed:
		cloned.ClaimedAt = &at
	case IntentStatusRunning:
		cloned.ClaimedAt = &at
	case IntentStatusSucceeded, IntentStatusFailed:
		cloned.CompletedAt = &at
	}

	return cloned
}
