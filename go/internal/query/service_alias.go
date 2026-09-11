// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B4 root alias shim for #6060: type aliases and thin forwarders for the moved service family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// service_alias.go is the root alias shim for the service handler family
// (#6060, lane B B4). ServiceCatalogHandler and its method files moved to
// service/; the deployment-trace enrichment helpers moved to impacttrace/;
// shared bounds moved to querycontract. Names the rest of the program still
// spells `query.X` (handler wiring, cmd routers, serviceintelhttp, staying
// root callers and tests) alias here so the move touches no caller outside
// the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import service directly.

// ServiceCatalogHandler is the service-catalog handler family type. Its home
// is service/; this alias keeps the APIRouter wiring and the cmd/api and
// cmd/mcp-server constructors spelling query.ServiceCatalogHandler
// unchanged. See #6060.
type ServiceCatalogHandler = service.ServiceCatalogHandler

// PostgresServiceCatalogCorrelationStore is the Postgres-backed service
// catalog correlation read model. Its home is service/; this alias keeps
// the cmd wirings spelling the query name unchanged. See #6060.
type PostgresServiceCatalogCorrelationStore = service.PostgresServiceCatalogCorrelationStore

// NewPostgresServiceCatalogCorrelationStore builds the Postgres-backed
// service-catalog correlation read model. Its home is service/; this
// variable (not a wrapper) keeps the cmd wirings spelling the
// query.NewPostgresServiceCatalogCorrelationStore name unchanged, and
// callers pass the database handle positionally so the unexported queryer
// parameter never needs naming outside its home. See #6060.
var NewPostgresServiceCatalogCorrelationStore = service.NewPostgresServiceCatalogCorrelationStore

// ServiceCatalogCorrelationStore reads reducer-owned service catalog
// correlations. Its canonical home is querycontract (via service/); this
// alias keeps the freshness, incident, repository, and codeowners stayers
// spelling query.ServiceCatalogCorrelationStore unchanged. See #6060.
type ServiceCatalogCorrelationStore = service.ServiceCatalogCorrelationStore

// ServiceCatalogCorrelationFilter bounds catalog reads. Its canonical home
// is querycontract (via service/); this alias keeps the freshness and
// incident stayers spelling the query name unchanged. See #6060.
type ServiceCatalogCorrelationFilter = service.ServiceCatalogCorrelationFilter

// ServiceCatalogCorrelationRow is one durable service-catalog correlation
// fact. Its canonical home is querycontract (via service/); this alias
// keeps the incident stayer spelling the query name unchanged. See #6060.
type ServiceCatalogCorrelationRow = service.ServiceCatalogCorrelationRow

// errServiceCatalogOutsideGrantNeedsAGrant refuses an outside-grant read
// that carries no grant at all. Its home is service/; this variable keeps
// the staying freshness stayers spelling the package-local name unchanged.
// See #6060.
var errServiceCatalogOutsideGrantNeedsAGrant = service.ErrServiceCatalogOutsideGrantNeedsAGrant

// ServiceWorkloadSelector is the exported selector for in-process
// service-story composers. Its home is service/; this alias keeps
// serviceintelhttp and the staying EntityHandler seam spelling the query
// name unchanged. See #6060.
type ServiceWorkloadSelector = service.ServiceWorkloadSelector

// ServiceQueryEvidence groups content-derived service evidence. Its home is
// service/; this alias keeps the staying compare handler spelling the query
// name unchanged. See #6060.
type ServiceQueryEvidence = service.ServiceQueryEvidence

// ServiceEvidenceReader is the content-store surface service evidence
// reads through. Its home is service/; this alias keeps the staying compare
// handler field spelling the query name unchanged. See #6060.
type ServiceEvidenceReader = service.ServiceEvidenceReader

// serviceEvidenceReader is the package-local spelling of
// ServiceEvidenceReader, kept for the staying compare handler field.
type serviceEvidenceReader = service.ServiceEvidenceReader

// FrameworkRouteEvidence is one framework route evidence shape. Its canonical
// home is querycontract (via service/); this alias keeps the staying
// content-reader stayer spelling the query name unchanged. See #6060.
type FrameworkRouteEvidence = service.FrameworkRouteEvidence

// FrameworkRouteEntryEvidence is one framework route entry. Its canonical
// home is querycontract (via service/); this alias keeps the staying
// content-reader stayer spelling the query name unchanged. See #6060.
type FrameworkRouteEntryEvidence = service.FrameworkRouteEntryEvidence

// ServiceAPISpecEvidence summarizes an OpenAPI spec file. Its canonical home
// is querycontract (via service/); this alias keeps staying callers spelling
// the query name unchanged. See #6060.
type ServiceAPISpecEvidence = service.ServiceAPISpecEvidence

// ServiceAPIEndpointEvidence summarizes one API endpoint. Its canonical home
// is querycontract (via service/); this alias keeps staying callers spelling
// the query name unchanged. See #6060.
type ServiceAPIEndpointEvidence = service.ServiceAPIEndpointEvidence

// serviceQueryEnrichmentOptions tunes the service query enrichment. Its home
// is service/; this alias keeps the staying entity handlers and the
// deployment-trace wrapper spelling the package-local name unchanged.
type serviceQueryEnrichmentOptions = service.ServiceQueryEnrichmentOptions

// serviceStoryImageCandidateParts is one parsed image-candidate shape. Its
// home is service/; this alias keeps the staying container-image
// explanation spelling the package-local name unchanged.
type serviceStoryImageCandidateParts = service.ServiceStoryImageCandidateParts

// serviceStoryItemLimit bounds service-story item reads. The canonical value
// lives in querycontract; this declaration keeps the staying supply-chain
// enricher compiling unchanged.
const serviceStoryItemLimit = querycontract.ServiceStoryItemLimit

// enrichServiceQueryContextWithOptions enriches a workload context with API
// surface, deployment evidence, and trace context. Its home is service/;
// this forwarder keeps the staying entity handlers and the service seam
// calling the package-local name.
func enrichServiceQueryContextWithOptions(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	workloadContext map[string]any,
	opts serviceQueryEnrichmentOptions,
) error {
	return service.EnrichServiceQueryContextWithOptions(ctx, graph, content, workloadContext, opts)
}

// enrichServiceQueryContext enriches a workload context with API surface,
// deployment evidence, and trace context using the canonical service-context
// defaults (related-module usage on, service_context operation). Its home is
// service/; this forwarder keeps the staying service enrichment tests
// calling the package-local name.
func enrichServiceQueryContext(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	workloadContext map[string]any,
) error {
	return service.EnrichServiceQueryContextWithOptions(ctx, graph, content, workloadContext, service.ServiceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Operation:                 "service_context",
	})
}

// serviceCatalogCorrelationFactKind is the fact kind carrying durable
// service-catalog correlation rows. Its home is service/; this alias keeps
// the staying supply-chain anchor test spelling the package-local name
// unchanged. See #6060.
const serviceCatalogCorrelationFactKind = service.ServiceCatalogCorrelationFactKind

// buildServiceStoryResponse assembles the service-story wire response. Its
// home is service/; this forwarder keeps the staying service seam and the
// staying answer-metadata and evidence-boundary tests calling the
// package-local name unchanged.
func buildServiceStoryResponse(serviceName string, workloadContext map[string]any) map[string]any {
	return service.BuildServiceStoryResponse(serviceName, workloadContext)
}

// loadServiceStoryTargetSupport loads the target-support section for a
// service-story workload context. Its home is service/; this forwarder
// keeps the staying interface-export tripwire test calling the package-local
// name unchanged.
func loadServiceStoryTargetSupport(
	ctx context.Context,
	content ContentStore,
	workloadContext map[string]any,
) (map[string]any, error) {
	return service.LoadServiceStoryTargetSupport(ctx, content, workloadContext)
}

// containsString reports whether values holds candidate. The implementation
// lives in querycontract; this wrapper keeps the staying replatforming
// stayer calling the package-local name.
func containsString(values []string, candidate string) bool {
	return querycontract.ContainsString(values, candidate)
}

// appendUniqueString appends value to values unless already present. The
// implementation lives in querycontract; this wrapper keeps the staying
// impact seam test calling the package-local name.
func appendUniqueString(values *[]string, value string) {
	querycontract.AppendUniqueString(values, value)
}

// ServiceCatalogLocalDescriptorEvidenceRow is one local-descriptor evidence
// row behind the service-catalog correlations read. Its home is service/;
// this alias keeps the staying catalog authz test spelling the query name
// unchanged. See #6060.
type ServiceCatalogLocalDescriptorEvidenceRow = service.ServiceCatalogLocalDescriptorEvidenceRow

// indirectEvidenceHostnameLimit bounds the surviving hostname list behind
// consumer enrichment. Its home is impacttrace/; this declaration keeps the
// staying deployment-trace regression test compiling unchanged.
const indirectEvidenceHostnameLimit = impacttrace.IndirectEvidenceHostnameLimit

// loadProvisioningSourceChainsFromCandidates loads the provisioning source
// chains for the pre-read candidate slice. The implementation moved to
// impacttrace for #6060; this wrapper keeps the staying repo-ID tiebreak
// test calling the package-local name unchanged.
func loadProvisioningSourceChainsFromCandidates(
	ctx context.Context,
	content ContentStore,
	candidates []provisioningRepositoryCandidate,
) ([]map[string]any, error) {
	return impacttrace.LoadProvisioningSourceChainsFromCandidates(ctx, content, candidates)
}

// buildGraphDependents groups provisioning candidates by repository. Its
// home is service/; this wrapper keeps the staying repo-ID tiebreak test
// calling the package-local name unchanged.
func buildGraphDependents(candidates []impacttrace.ProvisioningRepositoryCandidate) []map[string]any {
	return service.BuildGraphDependents(candidates)
}

// documentationStoryReadLimit bounds documentation story reads. The canonical
// value lives in querycontract; this declaration keeps the staying
// documentation stayers compiling unchanged.
const documentationStoryReadLimit = querycontract.DocumentationStoryReadLimit

// serviceCatalogCorrelationMaxLimit bounds service-catalog correlation
// pages. The canonical value lives in querycontract; this declaration keeps
// the staying freshness stayer compiling unchanged.
const serviceCatalogCorrelationMaxLimit = querycontract.ServiceCatalogCorrelationMaxLimit
