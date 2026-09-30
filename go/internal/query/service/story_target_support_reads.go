// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file hosts the operation-gated target-support and target-documentation
// loaders behind the service-story enrichment (Issue #6060, lane B B4). They
// moved here from the query root (service_story_target_support.go and
// documentation_story_overview.go), whose *ContentReader evidence methods
// must stay in package query: Go requires methods to live with their
// receiver type. The staying ContentReader method keeps serving the same
// rows through the exported LoadServiceStoryTargetSupport home. Bodies are
// unchanged modulo package qualifiers and the export rename below.

// serviceStoryTargetSupportStore is the read-model surface the loaders need.
// It is structural: the staying *ContentReader satisfies it without
// importing this package.
type serviceStoryTargetSupportStore interface {
	ServiceStoryTargetSupportEvidence(
		context.Context,
		querycontract.ServiceStoryTargetSupportFilter,
	) (querycontract.ServiceStoryTargetSupportReadModel, error)
}

// repositoryDefinesWorkloadsCypher lists the Workloads a repository DEFINES,
// bounded at three rows: the gate only has to tell zero from one from
// two-or-more. The target workload sorts first, so a repository that defines
// the target among more workloads than the bound still returns it and reads
// as ambiguous rather than as "does not define the target". It anchors on the
// Repository.id unique index and expands the typed DEFINES edge, with no
// aggregate (profiled on Neo4j in
// docs/internal/evidence/7138-story-target-support-writer-keys.md).
var repositoryDefinesWorkloadsCypher = `MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
RETURN w.id AS id
ORDER BY CASE WHEN id = $workload_id THEN 0 ELSE 1 END, id
LIMIT ` + strconv.Itoa(querycontract.ServiceStoryRepositoryWorkloadReadLimit)

// TargetSupportLoad is the outcome of one service-story target-support load: the
// support block plus what the graph gate decided, so the enrichment stage event
// can say why a story shows no (or ambiguous) support.
type TargetSupportLoad struct {
	// Support is the support block, or nil when no store backs the read.
	Support map[string]any
	// RepositoryWorkloadCount and RepositoryDefinesTarget are the graph gate's
	// verdict handed to the content store. Both are zero-valued when the graph
	// was not read.
	RepositoryWorkloadCount int
	RepositoryDefinesTarget bool
	// RepositoryDefinesErr is the graph read failure, if any. A failed read
	// fails the gate closed and never fails the story.
	RepositoryDefinesErr error
}

// LoadServiceStoryTargetSupport loads the target-support section for a
// service-story workload context, or (nil, nil) when no store backs the
// read. Pinned by the staying interface-export tripwire test via the root
// forwarder, and by loadServiceStoryTargetSupportForOperation below.
func LoadServiceStoryTargetSupport(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	workloadContext map[string]any,
) (map[string]any, error) {
	load, err := loadServiceStoryTargetSupportOutcome(ctx, graph, content, workloadContext)
	return load.Support, err
}

func loadServiceStoryTargetSupportOutcome(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	workloadContext map[string]any,
) (TargetSupportLoad, error) {
	store, ok := content.(serviceStoryTargetSupportStore)
	if !ok || store == nil {
		return TargetSupportLoad{}, nil
	}
	repoID := querycontract.SafeStr(workloadContext, "repo_id")
	serviceID := querycontract.SafeStr(workloadContext, "id")
	if repoID == "" && serviceID == "" {
		return TargetSupportLoad{}, nil
	}
	filter := querycontract.ServiceStoryTargetSupportFilter{
		Repository: repoID,
		Limit:      querycontract.ServiceStoryTargetSupportLimit,
	}
	var load TargetSupportLoad
	if serviceID != "" {
		filter.TargetKind = "service"
		filter.TargetID = serviceID
		filter.ServiceID = serviceID
		load = readRepositoryDefinesGate(ctx, graph, workloadContext, repoID, serviceID)
		filter.RepositoryWorkloadCount = load.RepositoryWorkloadCount
		filter.RepositoryDefinesTarget = load.RepositoryDefinesTarget
	} else {
		filter.TargetKind = "repository"
		filter.TargetID = repoID
	}
	readModel, err := store.ServiceStoryTargetSupportEvidence(ctx, filter)
	if err != nil {
		return load, err
	}
	load.Support = readModel.Support
	return load, nil
}

// readRepositoryDefinesGate asks the canonical graph which Workloads the
// service's repository DEFINES, once per story, and reports whether serviceID is
// among them. Repository-linked support belongs to a service only through that
// edge: a repository can define several distinct Workloads, so a link to the
// repository is the service's own only when the repository defines exactly it.
//
// It fails closed, to a zero verdict the content store treats as "no link", when
// there is no graph, no repository id, an identity-only context (the story is
// built from the repository read model and the graph has no Workload), or the
// read errors. An error is returned in the outcome for the stage event and does
// not fail the story.
func readRepositoryDefinesGate(
	ctx context.Context,
	graph querycontract.GraphQuery,
	workloadContext map[string]any,
	repoID string,
	serviceID string,
) TargetSupportLoad {
	if graph == nil || repoID == "" ||
		strings.TrimSpace(querycontract.SafeStr(workloadContext, "materialization_status")) == "identity_only" {
		return TargetSupportLoad{}
	}
	rows, err := graph.Run(ctx, repositoryDefinesWorkloadsCypher, map[string]any{"repo_id": repoID, "workload_id": serviceID})
	if err != nil {
		return TargetSupportLoad{RepositoryDefinesErr: err}
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if id := strings.TrimSpace(querycontract.StringVal(row, "id")); id != "" {
			seen[id] = struct{}{}
		}
	}
	_, definesTarget := seen[serviceID]
	return TargetSupportLoad{RepositoryWorkloadCount: len(seen), RepositoryDefinesTarget: definesTarget}
}

func loadServiceStoryTargetSupportForOperation(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	workloadContext map[string]any,
	operation string,
) (TargetSupportLoad, error) {
	if strings.TrimSpace(operation) != "service_story" {
		return TargetSupportLoad{}, nil
	}
	return loadServiceStoryTargetSupportOutcome(ctx, graph, content, workloadContext)
}

func loadServiceStoryTargetDocumentationForOperation(
	ctx context.Context,
	content querycontract.ContentStore,
	workloadContext map[string]any,
	operation string,
) (map[string]any, error) {
	if strings.TrimSpace(operation) != "service_story" {
		return nil, nil
	}
	return loadServiceStoryTargetDocumentation(ctx, content, workloadContext)
}

func loadServiceStoryTargetDocumentation(
	ctx context.Context,
	content querycontract.ContentStore,
	workloadContext map[string]any,
) (map[string]any, error) {
	repoID := querycontract.SafeStr(workloadContext, "repo_id")
	serviceID := querycontract.SafeStr(workloadContext, "id")
	if repoID == "" && serviceID == "" {
		return nil, nil
	}
	filter := querycontract.DocumentationFindingFilter{
		Repository: repoID,
		Limit:      querycontract.DocumentationStoryReadLimit,
	}
	if serviceID != "" {
		filter.TargetKind = "service"
		filter.TargetID = serviceID
		filter.ServiceID = serviceID
	} else {
		filter.TargetKind = "repository"
		filter.TargetID = repoID
	}
	return querycontract.LoadStoryTargetDocumentation(ctx, content, filter)
}
