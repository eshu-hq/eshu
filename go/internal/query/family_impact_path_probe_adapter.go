// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query // production ImpactPathProbeBackend: decodes via root hop helpers over driver structs, not movable without exporting them (#6060).

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// impactPathProbeBackend is the production impact.ImpactPathProbeBackend: it
// decodes raw graph path projections (nodes(path) and relationships(path)
// values, which embed Neo4j driver structs) into anchors and hop provenance
// for the by-id impact reads. The driver-typed decoding stays in root because
// only the driver-owning files may import the graph driver (package AGENTS.md,
// depguard query-no-graph-driver); the plain-data shaping runs in impacttrace.
// ImpactHandler.PathProbe carries the production adapter; tests inject fakes
// through the same interface. See #6060.
type impactPathProbeBackend struct{}

// NewImpactPathProbeBackend returns the production path-probe backend for
// ImpactHandler wiring.
func NewImpactPathProbeBackend() impact.ImpactPathProbeBackend {
	return impactPathProbeBackend{}
}

// ResolveAnchor implements impact.ImpactPathProbeBackend.
func (impactPathProbeBackend) ResolveAnchor(
	ctx context.Context,
	reader querycontract.GraphQuery,
	idParam, id string,
) (*impacttrace.ResolvedImpactAnchor, error) {
	return impacttrace.ResolveImpactAnchorNode(ctx, reader, idParam, id)
}

// TraceHops implements impact.ImpactPathProbeBackend.
func (impactPathProbeBackend) TraceHops(relsRaw any) []map[string]any {
	return impacttrace.ImpactTraceHops(impactRelProvenanceList(relsRaw))
}

// DependencyHops implements impact.ImpactPathProbeBackend.
func (impactPathProbeBackend) DependencyHops(nodesRaw, relsRaw any) []map[string]any {
	return impacttrace.ImpactDependencyHops(impactNodeIdentityList(nodesRaw), impactRelProvenanceList(relsRaw))
}

// PathHasNodes implements impact.ImpactPathProbeBackend.
func (impactPathProbeBackend) PathHasNodes(nodesRaw any) bool {
	return len(impactNodeIdentityList(nodesRaw)) > 0
}

// ResourceInvestigationHops implements impact.ImpactPathProbeBackend.
func (impactPathProbeBackend) ResourceInvestigationHops(relsRaw any) []map[string]any {
	return resourceInvestigationHopList(relsRaw)
}
