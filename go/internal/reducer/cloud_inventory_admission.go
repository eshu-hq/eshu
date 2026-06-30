// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SourceLayer classifies which evidence layer a cloud-inventory record carries.
// Declared, applied, and observed are distinct inputs; the admission path keeps
// them separate so a provider observation never overwrites declared IaC truth.
type SourceLayer string

const (
	// SourceLayerDeclared marks source-controlled IaC intent (Terraform/GitOps
	// declaration) for a resource identity.
	SourceLayerDeclared SourceLayer = "declared"
	// SourceLayerApplied marks applied state evidence (Terraform state) for a
	// resource identity.
	SourceLayerApplied SourceLayer = "applied"
	// SourceLayerObserved marks direct provider control-plane observation for a
	// resource identity.
	SourceLayerObserved SourceLayer = "observed"
)

// ManagementOrigin records the strongest evidence layer that contributed to one
// admitted canonical resource. Declared outranks applied, which outranks
// observed, so observed provider facts never demote declared management truth.
type ManagementOrigin string

const (
	// ManagementOriginDeclared means at least one declared IaC layer admitted
	// the canonical resource.
	ManagementOriginDeclared ManagementOrigin = "declared"
	// ManagementOriginApplied means applied state, but no declared layer,
	// admitted the canonical resource.
	ManagementOriginApplied ManagementOrigin = "applied"
	// ManagementOriginObserved means only provider observation admitted the
	// canonical resource.
	ManagementOriginObserved ManagementOrigin = "observed"
)

// CloudInventoryRecord is one provider cloud-inventory source fact projected
// into the fields the shared admission path needs. RawIdentity is the provider
// raw identity preserved by the collector: an AWS ARN, a GCP Cloud Asset
// Inventory full resource name, or an Azure ARM resource id.
type CloudInventoryRecord struct {
	// Provider is the source provider token (aws, gcp, azure, ...).
	Provider string
	// FactKind is the provider-specific source fact kind, preserved unchanged.
	FactKind string
	// RawIdentity is the provider raw identity the reducer resolves into a uid.
	RawIdentity string
	// ResourceType is the provider resource type, kept as bounded evidence.
	ResourceType string
	// SourceLayer classifies the evidence layer of this record.
	SourceLayer SourceLayer
	// Attributes carries bounded redaction-safe typed-depth attributes from the
	// provider source fact (e.g. table_type, schema_field_count, kms_key_name).
	// Only observed-layer facts carry attributes; nil means no attributes.
	Attributes map[string]any
}

// AdmittedCloudResource is one canonical CloudResource identity admitted from
// one or more provider records that resolved to the same cloud_resource_uid.
type AdmittedCloudResource struct {
	// CloudResourceUID is the canonical shared identity.
	CloudResourceUID string
	// Provider is the normalized provider token.
	Provider string
	// RawIdentity is the provider raw identity that resolved the uid.
	RawIdentity string
	// ResourceType is the provider resource type evidence.
	ResourceType string
	// FactKinds lists the contributing provider source fact kinds, sorted.
	FactKinds []string
	// ManagementOrigin is the strongest contributing evidence layer.
	ManagementOrigin ManagementOrigin
	// HasDeclaredEvidence reports whether a declared layer contributed.
	HasDeclaredEvidence bool
	// HasAppliedEvidence reports whether an applied layer contributed.
	HasAppliedEvidence bool
	// HasObservedEvidence reports whether an observed layer contributed.
	HasObservedEvidence bool
	// TagValueFingerprints carries keyed tag value fingerprints (tag key ->
	// fingerprint marker) attached from tag-evidence facts that share this uid.
	// It is nil unless a TagEvidenceLoader contributed evidence; tag value text
	// is never present, only the keyed markers from the collector.
	TagValueFingerprints map[string]string
	// IdentityPolicyEvidence carries bounded identity-policy evidence attached
	// from provider identity-observation facts that share this uid. Principal,
	// client, object, and tenant values are keyed fingerprints only; raw GUIDs
	// and raw assignment scopes are never present.
	IdentityPolicyEvidence []CloudIdentityPolicyEvidence
	// IdentityPolicyEvidenceTruncated reports that more identity-policy evidence
	// existed for this resource than the bounded payload cap permits.
	IdentityPolicyEvidenceTruncated bool
	// ResourceChangeEvidence carries sanitized freshness rows attached from
	// provider resource-change facts that share this uid. It is nil unless a
	// ResourceChangeEvidenceLoader contributed evidence; raw provider locators
	// and raw actor ids are never present.
	ResourceChangeEvidence []CloudResourceChangeEvidence
	// ResourceChangeEvidenceTruncated reports whether resource-change freshness
	// rows were capped before persistence.
	ResourceChangeEvidenceTruncated bool
	// Attributes carries bounded redaction-safe typed-depth attributes from the
	// provider source fact, observed layer only. Only bounded safe control-plane
	// values are present; raw locators and secrets are never included. It is nil
	// unless a record carried attributes.
	Attributes map[string]any
}

// CloudInventoryAdmissionSummary counts the non-admitted resolution outcomes so
// ambiguous, unsupported, and unresolved identities are surfaced rather than
// fabricated into canonical truth.
type CloudInventoryAdmissionSummary struct {
	// Admitted counts records that resolved into a canonical uid.
	Admitted int
	// Ambiguous counts records whose raw identity was malformed for its provider.
	Ambiguous int
	// Unsupported counts records from a provider outside the shared keyspace.
	Unsupported int
	// Unresolved counts records whose raw identity was blank.
	Unresolved int
}

// CloudInventoryAdmissionWrite is the durable publication request for one
// cloud-inventory admission intent. Resources are the admitted canonical rows;
// Summary carries the counted non-admitted outcomes.
type CloudInventoryAdmissionWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Resources    []AdmittedCloudResource
	Summary      CloudInventoryAdmissionSummary
}

// CloudInventoryAdmissionWriteResult summarizes one durable admission write.
type CloudInventoryAdmissionWriteResult struct {
	// CanonicalIDs lists the canonical row ids written, sorted and stable.
	CanonicalIDs []string
	// CanonicalWrites is the number of canonical resource rows persisted.
	CanonicalWrites int
	// EvidenceSummary is a short operator-facing description.
	EvidenceSummary string
}

// CloudInventoryEvidenceLoader loads provider cloud-inventory source facts for
// one scope generation. Implementations must bound the load to the supplied
// scope and generation so stale generations cannot leak rows into a newer
// admission.
type CloudInventoryEvidenceLoader interface {
	// LoadCloudInventoryEvidence returns the provider cloud-inventory records in
	// scope for the given generation.
	LoadCloudInventoryEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]CloudInventoryRecord, error)
}

// CloudInventoryAdmissionWriter persists admitted canonical CloudResource rows
// and the counted non-admitted summary. The writer must be idempotent by
// canonical uid within the scope generation so reducer retries and concurrent
// workers converge on one row per uid instead of duplicating canonical truth.
type CloudInventoryAdmissionWriter interface {
	// WriteCloudInventoryAdmission persists one admission publication.
	WriteCloudInventoryAdmission(
		ctx context.Context,
		write CloudInventoryAdmissionWrite,
	) (CloudInventoryAdmissionWriteResult, error)
}

// CloudInventoryAdmissionHandler admits provider cloud-inventory facts into the
// shared canonical cloud_resource_uid keyspace. It is graph-neutral: it writes
// reducer-owned canonical read-model facts only and defers graph projection and
// the multi-cloud drift join to follow-up slices.
type CloudInventoryAdmissionHandler struct {
	// EvidenceLoader loads provider cloud-inventory source facts.
	EvidenceLoader CloudInventoryEvidenceLoader
	// Writer persists admitted canonical rows and the counted summary.
	Writer CloudInventoryAdmissionWriter
	// GenerationCheck, when set, supersedes stale generations before any load or
	// write so a superseded scan never publishes canonical rows.
	GenerationCheck GenerationFreshnessCheck
	// TagEvidenceLoader, when set, loads tag-evidence facts (e.g.
	// azure_tag_observation) whose fingerprints attach to the canonical resource
	// sharing their cloud_resource_uid. A nil loader leaves the admission path
	// unchanged, so the AWS/GCP resource path carries no tag fingerprints.
	TagEvidenceLoader CloudTagEvidenceLoader
	// IdentityPolicyEvidenceLoader, when set, loads provider identity-policy
	// evidence (currently azure_identity_observation) whose keyed fingerprints
	// attach to the canonical resource sharing their cloud_resource_uid. A nil
	// loader leaves the admission path unchanged.
	IdentityPolicyEvidenceLoader CloudIdentityPolicyEvidenceLoader
	// ResourceChangeEvidenceLoader, when set, loads provider resource-change
	// facts whose sanitized freshness evidence attaches to the canonical
	// resource sharing their cloud_resource_uid. A nil loader leaves the
	// admission path unchanged.
	ResourceChangeEvidenceLoader CloudResourceChangeEvidenceLoader
	// Instruments, when set, records bounded admission phase counts.
	Instruments *telemetry.Instruments
	// AdmissionDecisionWriter, when set, persists explainable shared admission
	// decisions after the canonical admission writer succeeds.
	AdmissionDecisionWriter AdmissionDecisionWriter
	// AdmissionDecisionNow supplies timestamps for shared admission decisions.
	AdmissionDecisionNow func() time.Time
}

// Handle executes one cloud-inventory admission intent.
func (h CloudInventoryAdmissionHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainCloudInventoryAdmission {
		return Result{}, fmt.Errorf(
			"cloud_inventory_admission handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.EvidenceLoader == nil {
		return Result{}, fmt.Errorf("cloud inventory evidence loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("cloud inventory admission writer is required")
	}

	if h.GenerationCheck != nil {
		current, err := h.GenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			return Result{}, fmt.Errorf("check cloud inventory generation freshness: %w", err)
		}
		if !current {
			return Result{
				IntentID:        intent.IntentID,
				Domain:          intent.Domain,
				Status:          ResultStatusSuperseded,
				EvidenceSummary: "cloud inventory admission skipped: generation superseded",
			}, nil
		}
	}

	records, err := h.EvidenceLoader.LoadCloudInventoryEvidence(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load cloud inventory evidence: %w", err)
	}

	resources, summary := admitCloudInventoryRecords(records)

	if h.TagEvidenceLoader != nil {
		tagRecords, err := h.TagEvidenceLoader.LoadCloudTagEvidence(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			return Result{}, fmt.Errorf("load cloud tag evidence: %w", err)
		}
		attachCloudTagEvidence(resources, tagRecords)
	}
	if h.ResourceChangeEvidenceLoader != nil {
		changeRecords, err := h.ResourceChangeEvidenceLoader.LoadCloudResourceChangeEvidence(
			ctx,
			intent.ScopeID,
			intent.GenerationID,
		)
		if err != nil {
			return Result{}, fmt.Errorf("load cloud resource change evidence: %w", err)
		}
		attachCloudResourceChangeEvidence(resources, changeRecords)
	}

	if h.IdentityPolicyEvidenceLoader != nil {
		identityRecords, err := h.IdentityPolicyEvidenceLoader.LoadCloudIdentityPolicyEvidence(
			ctx,
			intent.ScopeID,
			intent.GenerationID,
		)
		if err != nil {
			return Result{}, fmt.Errorf("load cloud identity policy evidence: %w", err)
		}
		attachCloudIdentityPolicyEvidence(resources, identityRecords)
	}

	writeResult, err := h.Writer.WriteCloudInventoryAdmission(ctx, CloudInventoryAdmissionWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Resources:    resources,
		Summary:      summary,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write cloud inventory admission: %w", err)
	}
	if err := h.writeCloudInventoryAdmissionDecisions(ctx, intent, records, writeResult); err != nil {
		return Result{}, err
	}

	h.recordAdmissionCounts(ctx, resources, summary)

	return Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"cloud inventory admitted=%d ambiguous=%d unsupported=%d unresolved=%d canonical_writes=%d",
			summary.Admitted,
			summary.Ambiguous,
			summary.Unsupported,
			summary.Unresolved,
			writeResult.CanonicalWrites,
		),
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

// admitCloudInventoryRecords resolves each record's provider identity and folds
// records that share a canonical uid into one admitted resource, preserving the
// evidence layer so declared truth is never demoted by an observed fact. The
// returned resources are sorted by uid for deterministic, replay-safe writes.
func admitCloudInventoryRecords(
	records []CloudInventoryRecord,
) ([]AdmittedCloudResource, CloudInventoryAdmissionSummary) {
	var summary CloudInventoryAdmissionSummary
	byUID := make(map[string]*AdmittedCloudResource)
	factKinds := make(map[string]map[string]struct{})

	for _, record := range records {
		resolution := cloudinventory.ResolveProviderIdentity(record.Provider, record.RawIdentity)
		switch resolution.Outcome {
		case cloudinventory.ResolutionOutcomeAdmitted:
			summary.Admitted++
			foldAdmittedRecord(byUID, factKinds, resolution, record)
		case cloudinventory.ResolutionOutcomeAmbiguous:
			summary.Ambiguous++
		case cloudinventory.ResolutionOutcomeUnsupported:
			summary.Unsupported++
		case cloudinventory.ResolutionOutcomeUnresolved:
			summary.Unresolved++
		}
	}

	resources := make([]AdmittedCloudResource, 0, len(byUID))
	for uid, resource := range byUID {
		resource.FactKinds = sortedKeys(factKinds[uid])
		resources = append(resources, *resource)
	}
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].CloudResourceUID < resources[j].CloudResourceUID
	})
	return resources, summary
}

// foldAdmittedRecord merges one admitted record into the canonical resource for
// its uid, raising the management origin only toward stronger evidence so a
// later observed record cannot demote a declared one.
func foldAdmittedRecord(
	byUID map[string]*AdmittedCloudResource,
	factKinds map[string]map[string]struct{},
	resolution cloudinventory.Resolution,
	record CloudInventoryRecord,
) {
	uid := resolution.CloudResourceUID
	resource, ok := byUID[uid]
	if !ok {
		resource = &AdmittedCloudResource{
			CloudResourceUID: uid,
			Provider:         resolution.Provider,
			RawIdentity:      record.RawIdentity,
			ResourceType:     record.ResourceType,
			ManagementOrigin: ManagementOriginObserved,
		}
		byUID[uid] = resource
		factKinds[uid] = make(map[string]struct{})
	}
	if resource.ResourceType == "" {
		resource.ResourceType = record.ResourceType
	}
	if record.FactKind != "" {
		factKinds[uid][record.FactKind] = struct{}{}
	}
	switch record.SourceLayer {
	case SourceLayerDeclared:
		resource.HasDeclaredEvidence = true
	case SourceLayerApplied:
		resource.HasAppliedEvidence = true
	default:
		resource.HasObservedEvidence = true
	}
	resource.ManagementOrigin = strongestManagementOrigin(resource)
	if len(record.Attributes) > 0 && len(resource.Attributes) == 0 {
		resource.Attributes = record.Attributes
	}
}

// strongestManagementOrigin returns the highest-precedence evidence layer that
// contributed to the resource: declared, then applied, then observed.
func strongestManagementOrigin(resource *AdmittedCloudResource) ManagementOrigin {
	switch {
	case resource.HasDeclaredEvidence:
		return ManagementOriginDeclared
	case resource.HasAppliedEvidence:
		return ManagementOriginApplied
	default:
		return ManagementOriginObserved
	}
}

// recordAdmissionCounts emits the bounded admission phase counters. Labels are
// provider and outcome only; no resource id, name, project, subscription, or
// ARN ever reaches a metric label.
func (h CloudInventoryAdmissionHandler) recordAdmissionCounts(
	ctx context.Context,
	resources []AdmittedCloudResource,
	summary CloudInventoryAdmissionSummary,
) {
	if h.Instruments == nil || h.Instruments.CloudInventoryAdmissions == nil {
		return
	}
	for _, resource := range resources {
		h.Instruments.CloudInventoryAdmissions.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionProvider, resource.Provider),
			attribute.String(telemetry.MetricDimensionOutcome, string(cloudinventory.ResolutionOutcomeAdmitted)),
		))
	}
	h.addOutcomeCount(ctx, cloudinventory.ResolutionOutcomeAmbiguous, summary.Ambiguous)
	h.addOutcomeCount(ctx, cloudinventory.ResolutionOutcomeUnsupported, summary.Unsupported)
	h.addOutcomeCount(ctx, cloudinventory.ResolutionOutcomeUnresolved, summary.Unresolved)
}

// addOutcomeCount records one non-admitted outcome bucket without a provider
// label, since non-admitted records may carry an unsupported provider token.
func (h CloudInventoryAdmissionHandler) addOutcomeCount(
	ctx context.Context,
	outcome cloudinventory.ResolutionOutcome,
	count int,
) {
	if count <= 0 {
		return
	}
	h.Instruments.CloudInventoryAdmissions.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOutcome, string(outcome)),
	))
}
