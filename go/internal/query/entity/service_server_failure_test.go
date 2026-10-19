// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// Service-route stages a server failure can come from.
const (
	serviceFailQuery      = "query"
	serviceFailEnrichment = "enrichment"
	serviceFailCICD       = "cicd"
	serviceFailSupply     = "supply chain"
)

type failingCICDRunCorrelationStore struct{ err error }

func (s failingCICDRunCorrelationStore) ListCICDRunCorrelations(context.Context, querycontract.CICDRunCorrelationFilter) ([]querycontract.CICDRunCorrelationRow, error) {
	return nil, s.err
}

type failingImageIdentityStore struct{ err error }

func (s failingImageIdentityStore) ListContainerImageIdentities(context.Context, supplychain.ContainerImageIdentityFilter) ([]supplychain.ContainerImageIdentityRow, error) {
	return nil, s.err
}

// serviceFailureHandler builds a handler whose service resolves to
// workload:api with one deployment image, and whose read for stage fails
// with err.
func serviceFailureHandler(stage string, err error) *Handler {
	workload := map[string]any{"id": "workload:api", "name": "api", "kind": "service", "repo_id": "repo://example/api", "repo_name": "api"}
	reader := graph.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case stage == serviceFailQuery:
				return nil, err
			case strings.Contains(cypher, "HAS_DEPLOYMENT_EVIDENCE") && stage == serviceFailEnrichment:
				return nil, err
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo://example/api", "repo_name": "api"}}, nil
			case strings.Contains(cypher, "collect(DISTINCT dr.id) as defining"), strings.Contains(cypher, "w.name = $service_name"):
				return []map[string]any{workload}, nil
			case strings.Contains(cypher, "HAS_DEPLOYMENT_EVIDENCE") &&
				strings.Contains(cypher, "(r:Repository {id: $repo_id})<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(artifact:EvidenceArtifact)"):
				return []map[string]any{serviceStoryDeploymentImageArtifact(serviceStoryTestImageRef)}, nil
			default:
				return nil, nil
			}
		},
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if stage == serviceFailQuery {
				return nil, err
			}
			if strings.Contains(cypher, "w.id = $workload_id") {
				return workload, nil
			}
			return nil, nil
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	switch stage {
	case serviceFailCICD:
		handler.CICDRunCorrelations = failingCICDRunCorrelationStore{err: err}
	case serviceFailSupply:
		handler.ContainerImageIdentities = failingImageIdentityStore{err: err}
		handler.SBOMAttachments = &serviceStorySBOMAttachmentStore{}
	}
	return handler
}

// TestServiceRoutesServerFailureAnswersFixedMessage is the #7626 regression
// for every 500 on the service context, investigation, and story routes. Each
// answered "<prefix>: <backend error>" and left the span untouched, and the
// story's ci/cd and supply-chain steps also turned a reader fence into a 500.
// Each must answer its fixed message with a span error, map a reader fence to
// the retryable 503, and answer a client cancel with 499 and no span error.
func TestServiceRoutesServerFailureAnswersFixedMessage(t *testing.T) {
	t.Parallel()

	sites := []struct {
		route   string
		stage   string
		message string
	}{
		{"/api/v0/services/api/context", serviceFailQuery, serviceContextQueryFailedMessage},
		{"/api/v0/services/api/context", serviceFailEnrichment, serviceContextEnrichmentFailedMessage},
		{"/api/v0/investigations/services/api", serviceFailQuery, serviceInvestigationQueryFailedMessage},
		{"/api/v0/investigations/services/api", serviceFailEnrichment, serviceInvestigationEnrichmentFailedMessage},
		{"/api/v0/services/api/story", serviceFailQuery, serviceStoryQueryFailedMessage},
		{"/api/v0/services/api/story", serviceFailEnrichment, serviceStoryEnrichmentFailedMessage},
		{"/api/v0/services/api/story", serviceFailCICD, serviceStoryCICDEvidenceFailedMessage},
		{"/api/v0/services/api/story", serviceFailSupply, serviceStorySupplyChainFailedMessage},
	}
	routes := make([]failure.Route, 0, len(sites))
	for _, site := range sites {
		routes = append(routes, entityFailureRoute(site.route+"/"+site.stage, http.MethodGet, site.route, "", site.message,
			func(err error) *Handler { return serviceFailureHandler(site.stage, err) }))
	}
	runEntityFailureRoutes(t, routes)
}
