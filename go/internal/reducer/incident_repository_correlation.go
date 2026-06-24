// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// IncidentRepositoryCorrelationOutcome names the reducer decision for one
// incident-routing-to-repository correlation. The outcomes mirror the
// exact/derived/ambiguous/rejected discipline of the service catalog
// correlation (service_catalog_correlation.go): only confident outcomes carry a
// durable repository edge; everything weaker stays provenance-only.
type IncidentRepositoryCorrelationOutcome string

const (
	// IncidentRepositoryCorrelationExact means one PagerDuty provider service id
	// resolved to exactly one applied Terraform-state service whose backend
	// locator is owned by exactly one repository. The provider id matched
	// directly (incident.Service.ID == applied provider_object_id), so the edge
	// is the strongest durable signal.
	IncidentRepositoryCorrelationExact IncidentRepositoryCorrelationOutcome = "exact"
	// IncidentRepositoryCorrelationDerived means the applied service resolved to
	// exactly one owning repository through the durable backend-locator join, but
	// the provider id matched only after deterministic normalization rather than
	// a raw equality. It is still durable and edge-bearing.
	IncidentRepositoryCorrelationDerived IncidentRepositoryCorrelationOutcome = "derived"
	// IncidentRepositoryCorrelationAmbiguous means the provider service id mapped
	// to applied services owned by more than one distinct repository (or the
	// backend resolver reported an ambiguous owner). No edge is emitted: a tenant
	// boundary cannot pick a winner.
	IncidentRepositoryCorrelationAmbiguous IncidentRepositoryCorrelationOutcome = "ambiguous"
	// IncidentRepositoryCorrelationUnresolved means the applied service exists but
	// no Eshu-known config repository owns its Terraform backend locator (state is
	// operator-owned outside the repo set, or the backend fact is not ingested
	// yet). No edge is emitted.
	IncidentRepositoryCorrelationUnresolved IncidentRepositoryCorrelationOutcome = "unresolved"
	// IncidentRepositoryCorrelationRejected means the routing signal is too weak
	// for a durable edge: a blank provider id, a name-fingerprint-only match, or a
	// non-service resource class. It is provenance only and never edge-bearing.
	IncidentRepositoryCorrelationRejected IncidentRepositoryCorrelationOutcome = "rejected"
)

// AppliedPagerDutyServiceRouting is one durable applied PagerDuty service
// routing fact, as read from the incident_routing.applied_pagerduty_resource
// fact store. It is the only repo-anchorable routing slot because it carries
// both the real PagerDuty provider service id and the Terraform backend locator
// that the tfstatebackend resolver maps to an owning repository. The declared
// and observed slots are excluded: declared has no provider id, and observed has
// no backend locator.
type AppliedPagerDutyServiceRouting struct {
	// FactID is the durable source fact id, recorded as provenance on the edge.
	FactID string
	// StableFactKey is the generation-independent durable fact key, preferred for
	// the correlation identity so the edge survives re-materialization.
	StableFactKey string
	// ProviderObjectID is the real PagerDuty provider service id
	// (incident.Service.ID). A blank value rejects the row: there is no durable
	// anchor without it.
	ProviderObjectID string
	// NameFingerprint is the sanitized name fingerprint. It is recorded as
	// provenance only and MUST NOT drive a durable edge: a name match is the
	// fuzzy signal correlation truth forbids as a tenant boundary.
	NameFingerprint string
	// BackendKind is the Terraform backend kind (s3, gcs, azurerm, ...) of the
	// state that applied this service. It is half of the durable repo join key.
	BackendKind string
	// LocatorHash is the version-agnostic backend locator hash. It is the other
	// half of the durable repo join key and matches the config-side
	// terraform_backends locator hash exactly.
	LocatorHash string
	// ProviderIDExact reports whether the incident provider id matched the applied
	// provider_object_id by raw equality (true) versus a normalized/derived match
	// (false). It distinguishes exact from derived outcomes.
	ProviderIDExact bool
}

// BackendRepositoryResolution is the durable outcome of resolving one Terraform
// backend locator to its owning config repository. It is the pure-builder
// projection of tfstatebackend.Resolver.ResolveConfigCommitForBackend: a single
// owning repo, an ambiguous owner, or no owner. Modeling it as data keeps the
// classification logic testable without a database.
type BackendRepositoryResolution struct {
	// RepositoryID is the single owning repository, set only when Outcome is a
	// confident single-owner resolution.
	RepositoryID string
	// Ambiguous reports that more than one distinct repository claimed the
	// backend locator (tfstatebackend.ErrAmbiguousBackendOwner). It forces an
	// ambiguous correlation outcome with no edge.
	Ambiguous bool
}

// IncidentRepositoryCorrelationDecision records one bounded incident-routing
// correlation decision. Only Exact and Derived decisions carry a non-empty
// RepositoryID and ProvenanceOnly=false; every weaker outcome leaves
// RepositoryID blank and ProvenanceOnly=true so a downstream scoped predicate
// stays fail-closed.
type IncidentRepositoryCorrelationDecision struct {
	Provider               string
	ProviderServiceID      string
	BackendKind            string
	LocatorHash            string
	RepositoryID           string
	Outcome                IncidentRepositoryCorrelationOutcome
	Reason                 string
	ProvenanceOnly         bool
	CandidateRepositoryIDs []string
	EvidenceFactIDs        []string
}

// IncidentRepositoryCorrelationWrite carries decisions for durable publication.
type IncidentRepositoryCorrelationWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Decisions    []IncidentRepositoryCorrelationDecision
}

// IncidentRepositoryCorrelationWriteResult summarizes durable correlation writes.
type IncidentRepositoryCorrelationWriteResult struct {
	FactsWritten    int
	EvidenceSummary string
}

// IncidentRepositoryCorrelationWriter persists reducer-owned incident-routing
// repository correlations.
type IncidentRepositoryCorrelationWriter interface {
	WriteIncidentRepositoryCorrelations(
		context.Context, IncidentRepositoryCorrelationWrite,
	) (IncidentRepositoryCorrelationWriteResult, error)
}

// AppliedPagerDutyServiceRoutingLoader loads the applied PagerDuty service
// routing facts for one scope generation. It is the seam that hides the fact
// store from the handler.
type AppliedPagerDutyServiceRoutingLoader interface {
	LoadAppliedPagerDutyServiceRouting(
		ctx context.Context, scopeID, generationID string,
	) ([]AppliedPagerDutyServiceRouting, error)
}

// BackendRepositoryResolver resolves one Terraform backend locator to its owning
// config repository. It is the narrow projection of tfstatebackend.Resolver the
// builder depends on, so the durable join is unit-testable without Postgres.
type BackendRepositoryResolver interface {
	ResolveBackendRepository(
		ctx context.Context, backendKind, locatorHash string,
	) (BackendRepositoryResolution, error)
}

// incidentRepositoryCorrelationOutcomes lists every outcome in deterministic
// order for counter emission and summaries.
func incidentRepositoryCorrelationOutcomes() []IncidentRepositoryCorrelationOutcome {
	return []IncidentRepositoryCorrelationOutcome{
		IncidentRepositoryCorrelationExact,
		IncidentRepositoryCorrelationDerived,
		IncidentRepositoryCorrelationAmbiguous,
		IncidentRepositoryCorrelationUnresolved,
		IncidentRepositoryCorrelationRejected,
	}
}

func incidentRepositoryCorrelationCounts(
	decisions []IncidentRepositoryCorrelationDecision,
) map[IncidentRepositoryCorrelationOutcome]int {
	counts := make(map[IncidentRepositoryCorrelationOutcome]int, len(incidentRepositoryCorrelationOutcomes()))
	for _, decision := range decisions {
		counts[decision.Outcome]++
	}
	return counts
}

func incidentRepositoryCorrelationSummary(
	evaluated int,
	counts map[IncidentRepositoryCorrelationOutcome]int,
	factsWritten int,
) string {
	return fmt.Sprintf(
		"incident repository correlation evaluated=%d exact=%d derived=%d ambiguous=%d unresolved=%d rejected=%d facts_written=%d",
		evaluated,
		counts[IncidentRepositoryCorrelationExact],
		counts[IncidentRepositoryCorrelationDerived],
		counts[IncidentRepositoryCorrelationAmbiguous],
		counts[IncidentRepositoryCorrelationUnresolved],
		counts[IncidentRepositoryCorrelationRejected],
		factsWritten,
	)
}

// IncidentRepositoryCorrelationHandler correlates applied PagerDuty incident
// routing to owning repositories through the durable backend-locator join,
// emitting a repository edge only for confident single-owner resolutions. It
// never lets a PagerDuty service name create repository truth: the name
// fingerprint is provenance only.
type IncidentRepositoryCorrelationHandler struct {
	Loader      AppliedPagerDutyServiceRoutingLoader
	Resolver    BackendRepositoryResolver
	Writer      IncidentRepositoryCorrelationWriter
	Provider    string
	Instruments *telemetry.Instruments
}

// Handle executes one incident-repository correlation reducer intent.
func (h IncidentRepositoryCorrelationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainIncidentRepositoryCorrelation {
		return Result{}, fmt.Errorf(
			"incident_repository_correlation handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.Loader == nil {
		return Result{}, fmt.Errorf("incident repository correlation loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("incident repository correlation writer is required")
	}

	rows, err := h.Loader.LoadAppliedPagerDutyServiceRouting(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return Result{}, fmt.Errorf("load applied pagerduty service routing: %w", err)
	}
	provider := strings.TrimSpace(h.Provider)
	if provider == "" {
		provider = defaultIncidentProvider
	}
	decisions, err := BuildIncidentRepositoryCorrelations(ctx, provider, rows, h.Resolver)
	if err != nil {
		return Result{}, fmt.Errorf("build incident repository correlations: %w", err)
	}
	counts := incidentRepositoryCorrelationCounts(decisions)
	writeResult, err := h.Writer.WriteIncidentRepositoryCorrelations(ctx, IncidentRepositoryCorrelationWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write incident repository correlations: %w", err)
	}
	h.emitCounters(ctx, counts)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainIncidentRepositoryCorrelation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: incidentRepositoryCorrelationSummary(len(decisions), counts, writeResult.FactsWritten),
		CanonicalWrites: writeResult.FactsWritten,
	}, nil
}

const defaultIncidentProvider = "pagerduty"

func (h IncidentRepositoryCorrelationHandler) emitCounters(
	ctx context.Context,
	counts map[IncidentRepositoryCorrelationOutcome]int,
) {
	if h.Instruments == nil || h.Instruments.IncidentRepositoryCorrelations == nil {
		return
	}
	for _, outcome := range incidentRepositoryCorrelationOutcomes() {
		if counts[outcome] == 0 {
			continue
		}
		h.Instruments.IncidentRepositoryCorrelations.Add(ctx, int64(counts[outcome]), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainIncidentRepositoryCorrelation)),
			telemetry.AttrOutcome(string(outcome)),
		))
	}
}
