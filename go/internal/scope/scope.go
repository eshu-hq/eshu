// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import (
	"fmt"
	"strings"
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
)

// CollectorKind identifies the collector family that owns the scope.
type CollectorKind string

const (
	// CollectorGit represents the Git repository collector.
	CollectorGit CollectorKind = "git"
	// CollectorAWS represents the cloud inventory collector.
	CollectorAWS CollectorKind = "aws"
	// CollectorAzure represents the read-only Azure cloud inventory collector.
	// It observes Azure control-plane metadata through Azure Resource Graph for
	// one bounded tenant, subscription, or management-group shard and emits
	// provider-specific source facts; it never mutates Azure state.
	CollectorAzure CollectorKind = "azure"
	// CollectorGCP represents the read-only Google Cloud Asset Inventory
	// collector. It observes control-plane resource metadata through Cloud Asset
	// Inventory and emits redacted source facts; it never reads data-plane content.
	CollectorGCP CollectorKind = "gcp"
	// CollectorTerraformState represents the Terraform state collector.
	CollectorTerraformState CollectorKind = "terraform_state"
	// CollectorWebhook represents the event/webhook collector.
	CollectorWebhook CollectorKind = "webhook"
	// CollectorDocumentation represents the documentation source collector.
	CollectorDocumentation CollectorKind = "documentation"
	// CollectorOCIRegistry represents the OCI registry collector.
	CollectorOCIRegistry CollectorKind = "oci_registry"
	// CollectorPackageRegistry represents the package registry collector.
	CollectorPackageRegistry CollectorKind = "package_registry"
	// CollectorVulnerabilityIntelligence represents the vulnerability source collector.
	CollectorVulnerabilityIntelligence CollectorKind = "vulnerability_intelligence"
	// CollectorSBOMAttestation represents the hosted SBOM and attestation collector.
	CollectorSBOMAttestation CollectorKind = "sbom_attestation"
	// CollectorSecurityAlert represents hosted provider security-alert collectors.
	CollectorSecurityAlert CollectorKind = "security_alert"
	// CollectorCICDRun represents hosted CI/CD workflow-run collectors.
	CollectorCICDRun CollectorKind = "ci_cd_run"
	// CollectorPagerDuty represents hosted PagerDuty incident evidence collectors.
	CollectorPagerDuty CollectorKind = "pagerduty"
	// CollectorJira represents Jira work-item evidence collectors.
	CollectorJira CollectorKind = "jira"
	// CollectorScannerWorker represents isolated security analyzer workers.
	CollectorScannerWorker CollectorKind = "scanner_worker"
	// CollectorSemanticExtraction represents optional semantic extraction jobs
	// that emit model-assisted observations and hints as provenance only.
	CollectorSemanticExtraction CollectorKind = "semantic_extraction"
	// CollectorKubernetesLive represents the read-only Kubernetes live cluster
	// collector. It observes a configured cluster's API server with read-only
	// credentials and emits typed source facts; it never mutates the cluster.
	CollectorKubernetesLive CollectorKind = "kubernetes_live"
	// CollectorVaultLive represents the read-only Vault metadata collector for
	// the secrets/IAM posture lane. It observes a configured Vault cluster's
	// metadata endpoints with a read-only token and emits redacted source facts;
	// it never reads a secret value.
	CollectorVaultLive CollectorKind = "vault_live"
	// CollectorPrometheusMimir represents the read-only Prometheus/Grafana Mimir
	// metric metadata collector. It polls configured metric API targets for
	// bounded target and rule metadata and emits redacted source facts.
	CollectorPrometheusMimir CollectorKind = "prometheus_mimir"
	// CollectorTempo represents the read-only Grafana Tempo observability
	// collector. It polls a configured Tempo query API for bounded trace-signal
	// metadata and emits redacted source facts; it never reads trace payloads.
	CollectorTempo CollectorKind = "tempo"
	// CollectorGrafana represents the read-only Grafana observability metadata
	// collector. It observes a configured Grafana API target with a read-only
	// token and emits bounded folder, dashboard, datasource, and alert-rule
	// source facts; it never mutates the Grafana instance.
	CollectorGrafana CollectorKind = "grafana"
	// CollectorLoki represents the read-only Grafana Loki observability
	// collector. It polls a configured Loki API target with read-only
	// credentials and emits bounded, redacted log-signal metadata facts; it
	// never reads log payloads.
	CollectorLoki CollectorKind = "loki"
)

// AllCollectorKinds returns every collector kind known to the platform in a
// stable, deterministic order. It is the single source of truth for tooling
// that must enumerate the full collector fleet (readiness reports, promotion
// proofs, fleet hygiene checks) so adding a collector means updating one list.
func AllCollectorKinds() []CollectorKind {
	return []CollectorKind{
		CollectorGit,
		CollectorAWS,
		CollectorAzure,
		CollectorGCP,
		CollectorTerraformState,
		CollectorWebhook,
		CollectorDocumentation,
		CollectorOCIRegistry,
		CollectorPackageRegistry,
		CollectorVulnerabilityIntelligence,
		CollectorSBOMAttestation,
		CollectorSecurityAlert,
		CollectorCICDRun,
		CollectorPagerDuty,
		CollectorJira,
		CollectorScannerWorker,
		CollectorSemanticExtraction,
		CollectorKubernetesLive,
		CollectorVaultLive,
		CollectorPrometheusMimir,
		CollectorTempo,
		CollectorGrafana,
		CollectorLoki,
	}
}

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
