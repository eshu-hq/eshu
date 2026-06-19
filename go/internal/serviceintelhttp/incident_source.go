package serviceintelhttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// incidentContextCapability is the query capability whose truth contract governs
// the report's incidents_support section. It mirrors the get_incident_context
// surface so the section carries incident-context truth (durable routing facts),
// not the service-story platform truth. It must stay a capability present in the
// capability matrix (specs/capability-matrix.v1.yaml); query.BuildTruthEnvelope
// panics otherwise.
const incidentContextCapability = "incident.context.read"

// IncidentEvidenceSource loads durable, service-scoped incident routing records
// for a workload. Implementations resolve the workload's catalog service id and
// load its incident evidence; this seam keeps the report handler decoupled from
// the storage and reducer layers and testable without a database.
type IncidentEvidenceSource interface {
	// IncidentRecordsForWorkload returns the durable incident records routed to
	// the workload's catalog service, or nil when the workload does not resolve
	// to a single catalog service or has no routed incidents. It returns an error
	// only for an infrastructure failure, never to signal "no incidents".
	// Implementations own logging their own ambiguity and infrastructure outcomes
	// (they hold the richest context); the caller treats a returned error purely
	// as "leave the section unsupported".
	IncidentRecordsForWorkload(ctx context.Context, workloadID string) ([]serviceintel.IncidentRecord, error)
}

// catalogServiceIDResolver resolves a workload id to its durable catalog service
// id. postgres.ServiceCatalogIDResolver implements it.
type catalogServiceIDResolver interface {
	ResolveCatalogServiceID(ctx context.Context, workloadID string) (string, error)
}

// serviceIncidentEvidenceLoader loads durable incident routing evidence keyed by
// catalog service id, bounded to rowLimit rows so a report request cannot scan an
// unbounded incident history. postgres.ServiceIncidentEvidenceLoader implements it.
type serviceIncidentEvidenceLoader interface {
	GetIncidentEvidenceForServicesBounded(ctx context.Context, serviceIDs []string, rowLimit int) (map[string][]reducer.ServiceIncidentRecord, error)
}

// reportIncidentEvidenceRowLimit caps the incident-routing evidence rows the
// report read loads for one service. It is deliberately far above the report's
// surfaced incident bound (serviceintel.maxReportIncidents) and the few evidence
// slots per incident, so the read is bounded against a pathological incident
// history while still yielding more than enough distinct incidents for the
// composer's truncation detection to fire honestly when the service overflows
// the surfaced bound.
const reportIncidentEvidenceRowLimit = 512

// DurableIncidentEvidenceSource is the production IncidentEvidenceSource: it
// resolves the workload's durable catalog service id, then loads that service's
// durable incident routing evidence. It attributes incidents only when the
// workload resolves to exactly one catalog service, so ambiguous catalog
// ownership never mis-attributes incidents.
type DurableIncidentEvidenceSource struct {
	resolver catalogServiceIDResolver
	loader   serviceIncidentEvidenceLoader
	logger   *slog.Logger
}

// NewDurableIncidentEvidenceSource constructs the durable incident source over a
// catalog-service-id resolver and an incident-evidence loader (both Postgres
// backed in production). A nil logger is tolerated.
func NewDurableIncidentEvidenceSource(
	resolver catalogServiceIDResolver,
	loader serviceIncidentEvidenceLoader,
	logger *slog.Logger,
) DurableIncidentEvidenceSource {
	return DurableIncidentEvidenceSource{resolver: resolver, loader: loader, logger: logger}
}

// IncidentRecordsForWorkload resolves the workload to its catalog service id and
// loads that service's durable incident routing records, mapping them onto the
// composer's IncidentRecord. Ambiguous catalog ownership and an unresolved
// workload both yield nil records with a nil error (the section stays
// unsupported, never a fabricated "no incidents"); only an infrastructure
// failure returns an error.
func (s DurableIncidentEvidenceSource) IncidentRecordsForWorkload(
	ctx context.Context,
	workloadID string,
) ([]serviceintel.IncidentRecord, error) {
	catalogServiceID, err := s.resolver.ResolveCatalogServiceID(ctx, workloadID)
	if err != nil {
		if errors.Is(err, postgres.ErrAmbiguousCatalogService) {
			// Ambiguous catalog ownership: do not attribute incidents to an
			// arbitrary service. Surface it to the operator and report no records.
			s.warn(ctx, "service intelligence report skipped incidents for ambiguous catalog service",
				"serviceintel.incident_ambiguous_catalog_service", workloadID, "")
			return nil, nil
		}
		s.warn(ctx, "service intelligence report incident resolve failed",
			"serviceintel.incident_load_error", workloadID, "")
		return nil, fmt.Errorf("resolve catalog service id: %w", err)
	}
	if catalogServiceID == "" {
		return nil, nil
	}

	byService, err := s.loader.GetIncidentEvidenceForServicesBounded(ctx, []string{catalogServiceID}, reportIncidentEvidenceRowLimit)
	if err != nil {
		s.warn(ctx, "service intelligence report incident load failed",
			"serviceintel.incident_load_error", workloadID, catalogServiceID)
		return nil, fmt.Errorf("load incident evidence: %w", err)
	}
	return mapIncidentRecords(byService[catalogServiceID]), nil
}

// warn emits one nil-safe operator log for an incident-lane outcome. The source
// is the single logging authority for the lane because it has the richest
// context (the workload id and, once resolved, the catalog service id); callers
// treat a returned error purely as "leave the section unsupported".
func (s DurableIncidentEvidenceSource) warn(ctx context.Context, msg, event, workloadID, catalogServiceID string) {
	if s.logger == nil {
		return
	}
	attrs := []any{slog.String("event", event), slog.String("workload_id", workloadID)}
	if catalogServiceID != "" {
		attrs = append(attrs, slog.String("catalog_service_id", catalogServiceID))
	}
	s.logger.WarnContext(ctx, msg, attrs...)
}

// mapIncidentRecords projects durable reducer incident rows onto the composer's
// minimal IncidentRecord. It returns nil for an empty input so the caller leaves
// the section unsupported rather than supplying an empty supported section.
func mapIncidentRecords(rows []reducer.ServiceIncidentRecord) []serviceintel.IncidentRecord {
	if len(rows) == 0 {
		return nil
	}
	records := make([]serviceintel.IncidentRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, serviceintel.IncidentRecord{
			Provider:           row.Provider,
			ProviderIncidentID: row.ProviderIncidentID,
			TruthLabel:         row.TruthLabel,
		})
	}
	return records
}

// buildReportInput adapts the service-story dossier into the report input and,
// when an incident source is wired and the subject workload resolves to durable
// incident evidence, appends the incidents_support section carrying its own
// incident-context truth. A source error (logged by the source) leaves the
// section unsupplied — Compose surfaces it as unsupported with a fallback — so a
// failure in the incident lane never corrupts or fails the rest of the report.
func buildReportInput(
	ctx context.Context,
	dossier map[string]any,
	truth *query.TruthEnvelope,
	incidentSource IncidentEvidenceSource,
	supplyChainSources ...SupplyChainEvidenceSource,
) serviceintel.ReportInput {
	input := serviceintel.FromServiceStory(dossier, truth)

	workloadID := strings.TrimSpace(input.Subject.ServiceID)
	if workloadID == "" {
		return input
	}

	if len(supplyChainSources) > 0 && supplyChainSources[0] != nil {
		inventory, err := supplyChainSources[0].SupplyChainInventoryForWorkload(ctx, workloadID)
		if err == nil && len(inventory) > 0 {
			input.Sections = append(input.Sections, serviceintel.FromSupplyChainInventory(inventory, input.Subject, supplyChainTruth(truth)))
		}
	}

	if incidentSource != nil {
		records, err := incidentSource.IncidentRecordsForWorkload(ctx, workloadID)
		if err == nil && len(records) > 0 {
			input.Sections = append(input.Sections, serviceintel.FromIncidentEvidence(records, input.Subject, incidentTruth(truth)))
		}
	}
	return input
}

// supplyChainTruth builds the supply_chain section's truth from the
// supply-chain-impact aggregate capability, reusing the resolved profile of the
// service-story truth so the section reflects reducer-owned impact facts rather
// than platform service-story truth.
func supplyChainTruth(storyTruth *query.TruthEnvelope) *query.TruthEnvelope {
	profile := query.ProfileProduction
	if storyTruth != nil && storyTruth.Profile != "" {
		profile = storyTruth.Profile
	}
	return query.BuildTruthEnvelope(
		profile,
		supplyChainImpactAggregateCapability,
		query.TruthBasisSemanticFacts,
		"resolved from reducer-owned supply-chain impact facts; provider APIs are not called",
	)
}

// incidentTruth builds the incidents_support section's truth from the
// incident-context capability, reusing the resolved profile of the service-story
// truth so the section reflects incident-context truth rather than the platform
// service-story truth.
func incidentTruth(storyTruth *query.TruthEnvelope) *query.TruthEnvelope {
	profile := query.ProfileProduction
	if storyTruth != nil && storyTruth.Profile != "" {
		profile = storyTruth.Profile
	}
	return query.BuildTruthEnvelope(
		profile,
		incidentContextCapability,
		query.TruthBasisSemanticFacts,
		"resolved from durable incident-routing correlation facts via the incident-repository join; provider APIs are not called",
	)
}
