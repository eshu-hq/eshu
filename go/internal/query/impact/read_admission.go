// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ImpactReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the impact family reads (the 13 W5
// impact routes). Canonical reads persist through explicit core-owned
// admission contracts: eshu-api keeps the HTTP shape and calls this
// versioned port, which the core-owned graph, content, topic, live,
// and ECS stores satisfy jointly, composed by the consumer. A
// breaking change to the read shape fails the frozen method-set test
// until that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const ImpactReadAdmissionContractVersion = "v0"

// ImpactReadAdmission is the versioned admission-contract port the API
// handler will depend on instead of the direct store handles. It
// declares exactly the fourteen reads the 13 W5 impact routes admit
// through — the graph reads (Run, RunSingle, exercised directly, by
// the selector and workload-candidate resolution, and by the path
// decoders through the passed reader), the eight content reads the
// family closure proves exercised (GetEntityContent on the
// exposure-path anchor hydration, SearchEntitiesByName on the
// deployment-trace resource match, ListRepoEntities on the controller
// and semantic scans plus the provisioning chains, ListRepoEntitiesByType
// on the ECS count scan, ListRepoEntitiesByPaths on the change-surface
// path symbols, ListRepoEntitiesByIDs on the deployment-trace
// rehydration, ListRepoK8sSelectCandidates on the SELECTS candidate
// scan, SearchEntitiesByExactNameAnyRepo on the service-not-found
// reference leg), the code-topic investigation the change-surface root
// adapter runs (InvestigateCodeTopic via the ContentStore assertion in
// fetchChangeSurfaceTopicRows), the live pod-template reads
// (HasLiveIdentityMatch, ListLiveIdentityMatches), and the ECS task
// read (ListRunningTasks) — so the boundary is held honest by the
// compile-time production mirror (root
// impact_read_admission_mirror.go) plus the frozen method-set contract
// test. It will back the eshu-api extraction handoff for this family.
// The seam is read-only on purpose: every declared method is a bounded
// read and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve the impact reads but
// cannot acquire a write it did not bargain for through it.
//
// W-wave consumer: W5c-e impact legs (the 13 W5 impact POSTs), with an
// explicit dual-pin cite for the deployment-trace enrichment leg:
// trace-deployment-chain and deployment-config-influence require
// EntityReadAdmission (extended Mint A) alongside this pin for their
// TraceContext leg, which pulls the F-A service-story closure through
// the root adapter (entity workload context plus service enrichment).
// Silent reliance on the Entity pin is forbidden; this cite is the
// reliance, recorded here and in the Entity pin. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of
// the impact legs.
//
// Narrow by construction, recorded not silently assumed: the fourteen
// methods re-declare the port signatures in this impact-scoped shape
// instead of embedding or aliasing querycontract.GraphQuery,
// querycontract.ContentStore,
// codequery.CodeTopicContentInvestigator,
// deployment.KubernetesPodTemplateStore, or deployment.ECSTaskStore.
// This pin therefore grants nothing to the other families that read
// through those ports — the entity 9, the repository story, and every
// other GraphQuery or ContentStore consumer keep their own per-route
// relay reasons until deliberate pins cover them. Citing Entity,
// CodeGraph, IaC, or CoreReadContractsVersion here is void.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - ChangeSurfaceCodeBackend.FetchCodeSurface,
//     DeploymentTraceContextProvider.FetchServiceTraceContext and
//     BuildServiceDeploymentOverview: core logic and adapter seams, not
//     reads. The topic search the code-surface leg exercises IS frozen
//     here (InvestigateCodeTopic); the service-story closure the trace
//     leg pulls IS frozen in the cited Entity pin.
//   - PathProbeBackend methods (ResolveAnchor, ResolveAnchorCandidates,
//     TraceHops, DependencyHops, PathHasNodes, PathNodes,
//     ResourceInvestigationHops): the production prober decodes raw
//     driver values, and only the query root may import the driver.
//     The anchor resolutions read through the passed GraphQuery, which
//     this pin already freezes as Run and RunSingle.
//   - BuildContentRelationships-style builders and every other
//     ContentStore method (SearchFileContent*, SearchEntityContent*,
//     SearchCodeCandidates, GetFileContent(s), ListRepoFiles,
//     NonBlankRepoFilePaths, ListRepoEntitiesByKeys,
//     ListRepoEntitiesByTypes, SearchEntitiesByLanguageAndType,
//     ListFrameworkRoutes, RepositoryCoverage, the language aggregates,
//     the catalog reads beyond the selector pair, ResolveRepository,
//     the context-coverage and files-indexed facets, ListCICDRunCorrelations):
//     no W5 impact path calls them. The enrichment tail reads that DO
//     fire on the trace leg (evidence files, framework routes,
//     consumer search, artifact hydration, read-model facets) ride the
//     cited Entity pin, not this one.
//   - MatchRepositories and ListRepositories: the impact selector legs
//     never resolve a repository catalog entry (workload selectors bind
//     graph rows; the service-not-found leg searches entities, not the
//     catalog), so there is no catalog shape to freeze here.
//   - Profile, capability gates, grant-before-limit checks, scoped-token
//     node-by-node filtering, and the fixed failure messages are
//     handler behavior, proven at byte-parity time and never frozen or
//     dropped — never described as matrix-driven.
//   - No serialization, no worker or batch reduction anywhere in this
//     lane.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type ImpactReadAdmission interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
	RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error)
	GetEntityContent(ctx context.Context, entityID string) (*querycontract.EntityContent, error)
	SearchEntitiesByName(ctx context.Context, repoID, entityType, name string, limit int) ([]querycontract.EntityContent, error)
	SearchEntitiesByExactNameAnyRepo(ctx context.Context, entityType, name string, limit int) ([]querycontract.EntityContent, error)
	ListRepoEntities(ctx context.Context, repoID string, limit int) ([]querycontract.EntityContent, error)
	ListRepoEntitiesByType(ctx context.Context, repoID, entityType string, limit int) ([]querycontract.EntityContent, error)
	ListRepoEntitiesByPaths(ctx context.Context, repoID string, relativePaths []string, limit int) ([]querycontract.EntityContent, error)
	ListRepoEntitiesByIDs(ctx context.Context, repoID string, entityIDs []string, limit int) ([]querycontract.EntityContent, error)
	ListRepoK8sSelectCandidates(ctx context.Context, repoID string, limit int) ([]querycontract.K8sSelectCandidate, error)
	InvestigateCodeTopic(ctx context.Context, req codequery.CodeTopicInvestigationRequest) ([]codequery.CodeTopicEvidenceRow, error)
	HasLiveIdentityMatch(ctx context.Context, filter deployment.KubernetesPodTemplateFilter) (bool, error)
	ListLiveIdentityMatches(ctx context.Context, filter deployment.KubernetesPodTemplateFilter) ([]deployment.LiveIdentityMatch, error)
	ListRunningTasks(ctx context.Context, serviceARN string, access querycontract.RepositoryAccessFilter) (*deployment.ECSServiceTaskEvidence, error)
}
