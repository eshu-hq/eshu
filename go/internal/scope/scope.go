// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// ScopeKind identifies the durable source scope family.
type ScopeKind string

const (
	// KindRepository represents a repository snapshot scope.
	KindRepository ScopeKind = "repository"
	// KindAccount represents a cloud account or subscription scope.
	KindAccount ScopeKind = "account"
	// KindRegion represents a cloud region scope.
	KindRegion ScopeKind = "region"
	// KindCluster represents a runtime or orchestration cluster scope.
	KindCluster ScopeKind = "cluster"
	// KindStateSnapshot represents a point-in-time state snapshot scope.
	KindStateSnapshot ScopeKind = "state_snapshot"
	// KindEventTrigger represents an event-driven freshness trigger scope.
	KindEventTrigger ScopeKind = "event_trigger"
	// KindDocumentationSource represents a documentation source scope.
	KindDocumentationSource ScopeKind = "documentation_source"
	// KindContainerRegistryRepository represents an OCI registry repository.
	KindContainerRegistryRepository ScopeKind = "container_registry_repository"
	// KindPackageRegistry represents a package registry target scope.
	KindPackageRegistry ScopeKind = "package_registry"
	// KindVulnerabilityIntelligence represents a vulnerability source scope.
	KindVulnerabilityIntelligence ScopeKind = "vulnerability_intelligence"
	// KindSBOMAttestation represents a hosted SBOM or attestation document scope.
	KindSBOMAttestation ScopeKind = "sbom_attestation"
	// KindSecurityAlert represents a hosted provider repository-alert scope.
	KindSecurityAlert ScopeKind = "security_alert"
	// KindCICDRun represents hosted CI/CD provider workflow-run evidence.
	KindCICDRun ScopeKind = "ci_cd_run"
	// KindPagerDutyAccount represents a PagerDuty account or service allowlist scope.
	KindPagerDutyAccount ScopeKind = "pagerduty_account"
	// KindJiraSite represents a Jira Cloud site work-item evidence scope.
	KindJiraSite ScopeKind = "jira_site"
	// KindScannerWorker represents a bounded security analyzer work scope.
	KindScannerWorker ScopeKind = "scanner_worker"
	// KindVaultCluster represents a HashiCorp Vault cluster (and namespace)
	// metadata scope for the secrets/IAM posture lane.
	KindVaultCluster ScopeKind = "vault_cluster"
	// KindRepositoryRef represents a non-default-branch repository snapshot scope
	// isolated from canonical graph projection. Ref-scoped scopes carry identity
	// repo_id@ref and their facts never reach the canonical graph (epic #5393,
	// enabler #5417). The projector gate rejects this kind by default; it is only
	// processed by ref-overlay consumers that explicitly opt in.
	KindRepositoryRef ScopeKind = "repository_ref"
)

// GitRepositoryScopePrefix is the scope id prefix the git collector writes
// for repository scopes: a default-branch scope is the prefix plus the
// canonical repository id, and a ref scope appends "@<ref>". Every package
// that builds or reads a git repository scope id names this constant, so one
// edit cannot silently split how packages classify a repository scope
// (#7879). recovery.GitRepositoryScopePrefix and (through it)
// querycontract.GitRepositoryScopePrefix alias this constant.
const GitRepositoryScopePrefix = "git-repository-scope:"

// TriggerKind identifies how a generation was produced.
type TriggerKind string

const (
	// TriggerKindSnapshot represents a snapshot-driven generation.
	TriggerKindSnapshot TriggerKind = "snapshot"
)

// GenerationStatus describes the lifecycle state of a scope generation.
type GenerationStatus string

const (
	// GenerationStatusPending means the generation exists but is not active yet.
	GenerationStatusPending GenerationStatus = "pending"
	// GenerationStatusActive means the generation is currently authoritative.
	GenerationStatusActive GenerationStatus = "active"
	// GenerationStatusSuperseded means a newer generation replaced this one.
	GenerationStatusSuperseded GenerationStatus = "superseded"
	// GenerationStatusCompleted means the generation finished successfully.
	GenerationStatusCompleted GenerationStatus = "completed"
	// GenerationStatusFailed means the generation finished unsuccessfully.
	GenerationStatusFailed GenerationStatus = "failed"
)

var allowedGenerationTransitions = map[GenerationStatus]map[GenerationStatus]struct{}{
	GenerationStatusPending: {
		GenerationStatusActive: {},
		GenerationStatusFailed: {},
	},
	GenerationStatusActive: {
		GenerationStatusSuperseded: {},
		GenerationStatusCompleted:  {},
		GenerationStatusFailed:     {},
	},
	GenerationStatusSuperseded: {},
	GenerationStatusCompleted:  {},
	GenerationStatusFailed:     {},
}

// IngestionScope is the durable identity for a source-local scope.
type IngestionScope struct {
	ScopeID       string
	SourceSystem  string
	ScopeKind     ScopeKind
	ParentScopeID string
	CollectorKind CollectorKind
	PartitionKey  string
	// ActiveGenerationID is the currently authoritative generation, if one
	// exists. This is not a reliable "prior generation exists" signal because
	// a failed or superseded prior generation may leave no active generation.
	ActiveGenerationID string
	// PreviousGenerationExists is true when the claimed generation is not the
	// first generation ever seen for this scope. Projection uses this to avoid
	// skipping cleanup after a failed first-generation attempt.
	PreviousGenerationExists bool
	Metadata                 map[string]string
}

// HasPriorGeneration reports whether this scope has any generation before the
// one currently being projected, including failed generations that were never
// promoted to ActiveGenerationID.
func (s IngestionScope) HasPriorGeneration() bool {
	return s.PreviousGenerationExists
}

// Validate checks that the scope has the minimum durable identity fields.
func (s IngestionScope) Validate() error {
	if err := validateIdentifier("scope_id", s.ScopeID); err != nil {
		return err
	}
	if err := validateIdentifier("source_system", s.SourceSystem); err != nil {
		return err
	}
	if err := validateIdentifier("scope_kind", string(s.ScopeKind)); err != nil {
		return err
	}
	if err := validateIdentifier("collector_kind", string(s.CollectorKind)); err != nil {
		return err
	}
	if err := validateIdentifier("partition_key", s.PartitionKey); err != nil {
		return err
	}
	if s.ParentScopeID != "" && s.ParentScopeID == s.ScopeID {
		return fmt.Errorf("parent_scope_id must differ from scope_id")
	}
	return nil
}

// MetadataCopy returns a defensive copy of the scope metadata map.
func (s IngestionScope) MetadataCopy() map[string]string {
	if len(s.Metadata) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(s.Metadata))
	for key, value := range s.Metadata {
		cloned[key] = value
	}
	return cloned
}

// ScopeGeneration is the durable truth boundary for one observed scope snapshot.
type ScopeGeneration struct {
	GenerationID  string
	ScopeID       string
	ObservedAt    time.Time
	IngestedAt    time.Time
	Status        GenerationStatus
	TriggerKind   TriggerKind
	FreshnessHint string
	// SourceCommitSHA records the source-control commit this generation was
	// observed from, when the collector can determine one (git scopes). It is
	// the durable baseline for incremental delta sync: the next sync diffs
	// against the SHA of the most recent generation that reached a projected
	// state, never against the local working-copy HEAD, so a projection that
	// fails after a checkout advanced HEAD cannot silently skip its changes.
	// Empty for scopes with no commit identity (filesystem, cloud collectors).
	SourceCommitSHA string
	// IsDelta marks a generation that carries only file-scoped changes (a delta
	// resync) rather than a full repository observation. The reconciliation sweep
	// uses the most recent full (IsDelta=false) projected generation per scope to
	// decide when a scope is overdue for a full re-observation that retracts any
	// drift the delta path missed (epic #2340).
	IsDelta bool
	// DeltaBaselineCommitSHA is the commit a delta generation's diff was taken
	// from (#7319): the active generation's commit when the collector read its
	// baseline. The projector refuses the delta unless that is still the active
	// commit. Set only when IsDelta is true; empty for full generations.
	DeltaBaselineCommitSHA string
	// DeltaUnionBaseCommitSHA is the commit of the newest activated full
	// generation a delta's changed set was diffed from (#8780): the projector
	// resolves the delta's union base at this commit, falling back to
	// DeltaBaselineCommitSHA for a delta stored before the column existed.
	// Set only when IsDelta is true; Validate rejects it on a full generation.
	DeltaUnionBaseCommitSHA string
	// ReconcileReason is the bounded reason a forced full reconciliation
	// snapshot was taken (epic #2340, #7978), for example interval_elapsed or
	// graph_dirty. Empty for an ordinary observation. It never changes
	// FreshnessHint: when the reconciliation snapshot matches the active
	// generation (same commit, same hint), the ingestion store re-projects that
	// generation instead of storing a duplicate, and records the reason in the
	// generation payload as reconcile_reason.
	ReconcileReason string
	// DefaultBranchChanged marks a full observation taken because the remote
	// default branch changed (#7678). The ingestion store always stores such a
	// generation, even when its hint and commit equal the active generation's,
	// so the repository fact's default branch re-projects.
	DefaultBranchChanged bool
	// TrustedDeletion is the collector's explicit attestation that an empty
	// or sharply shrunken successor is legitimate (for example a Jira
	// target mid mass-resolve, set from explicit operator configuration).
	// The ingestion store's successor-emptiness backstop (#7926) refuses to
	// replace a content-bearing generation with such a successor unless this
	// is set, keeping the prior generation active. It defaults to false,
	// is never persisted to scope_generations, and travels only in memory
	// from the collector to the commit that evaluates it.
	TrustedDeletion bool
	// DeltaUnionBaseGenerationID names the base full generation a delta's
	// projection input was overlaid on by the #7797 delta-base union. The
	// projector sets it from the same fact load that built the overlay,
	// just before Project, so the fact boundary accepts exactly the delta's
	// own facts and that base's facts (#8779). It is never persisted or
	// serialized, is set only on a delta, and never names the generation
	// itself; ValidateForScope rejects either misuse.
	DeltaUnionBaseGenerationID string `json:"-"`
	// DeltaUnionChained marks, for the build_projection stage log only, a
	// delta whose union base is not its fence commit's generation: it was
	// diffed over a prior delta in the chain (#8780). Set with
	// DeltaUnionBaseGenerationID from the same load; never persisted.
	DeltaUnionChained bool `json:"-"`
	// PreparseIdentity is the pre-parse identity of the snapshot this
	// generation came from (#7978 follow-up, ruling a9-143): the content
	// version, commit, default branch, and configuration that determine its
	// facts. The store keeps it on the generation so a later reconcile with an
	// equal identity can skip the parse. Empty when unknown.
	PreparseIdentity string
	// PreparseIdentityDropped, when set, is true after the fact stream has
	// drained if the stream dropped a file it could not read (review F-7,
	// ruling a9-143.14). The facts of such a generation may differ from a later
	// identical run, so the store records the drop in the payload instead of
	// trusting the identity blindly: the identity stays (a persistent drop
	// recurs identically, so the next equal snapshot may still skip, #8185),
	// and the skip probe re-checks PreparseIdentityDroppedPaths first. A
	// pointer keeps ScopeGeneration comparable; it is never persisted.
	PreparseIdentityDropped *atomic.Bool `json:"-"`
	// PreparseIdentityDroppedPaths accumulates the repository-relative paths
	// the fact stream dropped (#8185). The store persists Sorted in the
	// generation payload next to the stream_dropped mark; the collector's
	// skip probe reads them back through the active generation. A pointer
	// keeps ScopeGeneration comparable; it is never persisted directly.
	PreparseIdentityDroppedPaths *StreamDroppedPaths `json:"-"`
	// SecurityAlertTargetSet is the repository set a security-alert target
	// generation collected (#8838). The ingestion store persists it and its
	// hash in the generation payload; it is the zero value for every other
	// collector.
	SecurityAlertTargetSet SecurityAlertTargetSet
}

// Validate checks the generation fields and lifecycle status.
func (g ScopeGeneration) Validate() error {
	if err := validateIdentifier("generation_id", g.GenerationID); err != nil {
		return err
	}
	if err := validateIdentifier("scope_id", g.ScopeID); err != nil {
		return err
	}
	if err := validateTime("observed_at", g.ObservedAt); err != nil {
		return err
	}
	if err := validateTime("ingested_at", g.IngestedAt); err != nil {
		return err
	}
	if g.IngestedAt.Before(g.ObservedAt) {
		return fmt.Errorf("ingested_at must not be before observed_at")
	}
	if err := g.Status.Validate(); err != nil {
		return err
	}
	if err := validateIdentifier("trigger_kind", string(g.TriggerKind)); err != nil {
		return err
	}
	if g.DeltaUnionBaseCommitSHA != "" && !g.IsDelta {
		return fmt.Errorf("delta_union_base_commit_sha is set on non-delta generation %q", g.GenerationID)
	}
	return nil
}

// ValidateForScope ensures the generation belongs to the supplied scope.
func (g ScopeGeneration) ValidateForScope(scope IngestionScope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if err := g.Validate(); err != nil {
		return err
	}
	if g.ScopeID != scope.ScopeID {
		return fmt.Errorf("generation scope_id %q does not match scope scope_id %q", g.ScopeID, scope.ScopeID)
	}
	if base := g.DeltaUnionBaseGenerationID; base != "" {
		if !g.IsDelta {
			return fmt.Errorf("delta_union_base_generation_id %q is set on non-delta generation %q", base, g.GenerationID)
		}
		if base == g.GenerationID {
			return fmt.Errorf("delta_union_base_generation_id %q names the generation itself", base)
		}
	}
	return nil
}

// IsTerminal reports whether the generation cannot move to another status.
func (g ScopeGeneration) IsTerminal() bool {
	return g.Status.IsTerminal()
}

// CanTransitionTo reports whether the generation may move to the next status.
func (g ScopeGeneration) CanTransitionTo(next GenerationStatus) bool {
	_, ok := allowedGenerationTransitions[g.Status][next]
	return ok
}

// TransitionTo returns a copy with the requested status if the move is allowed.
func (g ScopeGeneration) TransitionTo(next GenerationStatus) (ScopeGeneration, error) {
	if err := g.Validate(); err != nil {
		return ScopeGeneration{}, err
	}
	if !g.CanTransitionTo(next) {
		return ScopeGeneration{}, fmt.Errorf("cannot transition generation status from %q to %q", g.Status, next)
	}

	g.Status = next
	return g, nil
}

// MarkActive promotes a pending generation to active.
func (g ScopeGeneration) MarkActive() (ScopeGeneration, error) {
	return g.TransitionTo(GenerationStatusActive)
}

// MarkCompleted marks an active generation as completed.
func (g ScopeGeneration) MarkCompleted() (ScopeGeneration, error) {
	return g.TransitionTo(GenerationStatusCompleted)
}

// MarkSuperseded marks an active generation as replaced by a newer one.
func (g ScopeGeneration) MarkSuperseded() (ScopeGeneration, error) {
	return g.TransitionTo(GenerationStatusSuperseded)
}

// MarkFailed marks a pending or active generation as failed.
func (g ScopeGeneration) MarkFailed() (ScopeGeneration, error) {
	return g.TransitionTo(GenerationStatusFailed)
}

// Validate checks that the generation status is known and stable.
func (status GenerationStatus) Validate() error {
	switch status {
	case GenerationStatusPending, GenerationStatusActive, GenerationStatusSuperseded, GenerationStatusCompleted, GenerationStatusFailed:
		return nil
	default:
		return fmt.Errorf("unknown generation status %q", status)
	}
}

// IsTerminal reports whether the status cannot transition to another state.
func (status GenerationStatus) IsTerminal() bool {
	switch status {
	case GenerationStatusSuperseded, GenerationStatusCompleted, GenerationStatusFailed:
		return true
	default:
		return false
	}
}

func validateIdentifier(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be blank", field)
	}
	return nil
}

func validateTime(field string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("%s must not be zero", field)
	}
	return nil
}
