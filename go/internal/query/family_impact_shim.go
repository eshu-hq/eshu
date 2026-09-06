// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// family_impact_shim.go is the root alias shim for the impact handler family
// (#6060). ImpactHandler and its method files moved to impact/; the
// deployment-trace helpers moved to impacttrace/; pure helpers moved to
// querycontract/. Names the rest of the program still spells `query.X`
// (handler wiring, cmd routers, external query_test consumers) alias here so
// the move touches no caller outside the family. Production backend wiring
// (seam func vars and Default backends) also lands here at init.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// assigns the canonical home.

// ImpactHandler is the impact investigation handler. Its home is impact/;
// this alias keeps handler wiring, cmd routers, and tests spelling
// query.ImpactHandler unchanged. See #6060.
type ImpactHandler = impact.ImpactHandler

// init wires the production backends behind the impact seam. Handlers built
// with zero-value backend fields (the cmd wirings) resolve through these
// defaults; tests inject fakes through the same interfaces or func vars.
// This assignment is the behavior-preservation proof for the move: without
// it the seam would serve nil backends and identity packets. See #6060.
func init() {
	impact.DefaultCodeSurface = NewChangeSurfaceCodeBackend()
	impact.DefaultTraceContext = NewDeploymentTraceContext()
	impact.DefaultPathProbe = NewImpactPathProbeBackend()
	impact.AttachDeveloperChangePlanPacket = attachDeveloperChangePlanPacket
	impact.AttachPreChangeImpactPacket = attachPreChangeImpactPacket
}

// uniqueStrings drops duplicates preserving order. Its home is impact/; this
// forwarder keeps the content-reader family (which stays in root per B3-T2
// and must not be touched for #6060) calling the package-local name.
func uniqueStrings(values []string) []string {
	return impact.UniqueStrings(values)
}

// NewPostgresKubernetesPodTemplateStore builds the Postgres-backed
// kubernetes_live.pod_template read model. Its home is impacttrace/; this
// variable (not a wrapper) keeps the cmd wirings spelling the
// query.NewPostgresKubernetesPodTemplateStore name unchanged, and callers
// pass the database handle positionally so the unexported queryer parameter
// never needs naming outside its home. See #6060.
var NewPostgresKubernetesPodTemplateStore = impacttrace.NewPostgresKubernetesPodTemplateStore

// containsAllSubstrings reports whether value contains every part. Its home
// is querytestutil; this forwarder keeps lane-A and content-reader staying
// tests (which must not be touched for #6060) calling the package-local
// name.
func containsAllSubstrings(value string, parts ...string) bool {
	return querycontract.ContainsAllSubstrings(value, parts...)
}

// TraceEnrichmentConfig tunes trace enrichment depth. Its home is impact/;
// this alias keeps the external query_test seam tripwire spelling
// query.TraceEnrichmentConfig unchanged. See #6060.
type TraceEnrichmentConfig = impact.TraceEnrichmentConfig

// NewTraceEnrichmentConfig builds the enrichment config covering the only
// cross-set construction site. Its home is impact/; this forwarder keeps the
// external query_test seam tripwire spelling query.NewTraceEnrichmentConfig
// unchanged. See #6060.
func NewTraceEnrichmentConfig(maxDepth int) TraceEnrichmentConfig {
	return impact.NewTraceEnrichmentConfig(maxDepth)
}

// FetchServiceTraceContext enriches one service trace. Its home is impact/;
// this forwarder keeps the external query_test seam tripwire spelling
// query.FetchServiceTraceContext unchanged. See #6060.
func FetchServiceTraceContext(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	logger *slog.Logger,
	serviceName string,
	traceOptions TraceEnrichmentConfig,
) (map[string]any, error) {
	return impact.FetchServiceTraceContext(ctx, graph, content, logger, serviceName, traceOptions)
}

// DeploymentSourceResult carries deployment-source rows and limits. Its home
// is impact/; this alias keeps the external query_test seam tripwire spelling
// query.DeploymentSourceResult unchanged. See #6060.
type DeploymentSourceResult = impact.DeploymentSourceResult

// FilterRowsByRepoIDForAccess scopes rows to the access grant. Its home is
// impact/; this forwarder keeps the external query_test seam tripwire spelling
// query.FilterRowsByRepoIDForAccess unchanged. See #6060.
func FilterRowsByRepoIDForAccess(rows []map[string]any, access querycontract.RepositoryAccessFilter) []map[string]any {
	return impact.FilterRowsByRepoIDForAccess(rows, access)
}

// K8sResourceResult carries bounded K8s resource rows. Its home is impact/;
// this alias keeps the external query_test seam tripwire spelling the result
// type unchanged. See #6060.
type K8sResourceResult = impact.K8sResourceResult

// BoundedK8sResourceResult merges content rows into the bounded K8s result.
// Its home is impact/; this forwarder keeps the external query_test seam
// tripwire spelling query.BoundedK8sResourceResult unchanged. See #6060.
func BoundedK8sResourceResult(
	contentRows []map[string]any,
	contentLowerBound bool,
	deploymentSourceRows []map[string]any,
	deploymentSourceLowerBound bool,
	selectCandidatePoolTruncated bool,
) K8sResourceResult {
	return impact.BoundedK8sResourceResult(contentRows, contentLowerBound, deploymentSourceRows, deploymentSourceLowerBound, selectCandidatePoolTruncated)
}
