// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestEvidenceRoutesAnswerFixedServerFailures is the #7674 slice 9 regression
// for the evidence and investigation packet reads that open a handler span.
// Each answered 500 with the store error text (err.Error()), answered a stale
// reader with that 500 instead of the retryable 503, and answered a client
// cancel as a server fault. The drift packet readback already had fixed text
// but skipped the fence and cancel rules. Every store here reads through the
// guarded PostgreSQL reader. Not parallel: it swaps queryHandlerTracer.
func TestEvidenceRoutesAnswerFixedServerFailures(t *testing.T) {
	const citationsPath = "/api/v0/evidence/citations"
	routes := []statusFailureRoute{
		{
			route: failure.Route{
				Name: "relationship evidence", Path: "/api/v0/evidence/relationships/resolved-1",
				Message: relationshipEvidenceReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &EvidenceHandler{Content: failingEvidenceContentStore{err: err}}
				},
			},
			capability: relationshipEvidenceCapability,
		},
		{
			route: failure.Route{
				Name: "citation files", Method: http.MethodPost, Path: citationsPath,
				Body:    `{"handles":[{"kind":"file","repo_id":"repo-a","relative_path":"main.go"}]}`,
				Message: evidenceCitationFileReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &EvidenceHandler{Content: failingEvidenceContentStore{err: err}}
				},
			},
			capability: evidenceCitationCapability,
		},
		{
			route: failure.Route{
				Name: "citation entities", Method: http.MethodPost, Path: citationsPath,
				Body:    `{"handles":[{"kind":"entity","entity_id":"entity-1"}]}`,
				Message: evidenceCitationEntityReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &EvidenceHandler{Content: failingEvidenceContentStore{err: err}}
				},
			},
			capability: evidenceCitationCapability,
		},
		{
			route: failure.Route{
				Name:    "deployable-unit packet",
				Path:    "/api/v0/investigations/deployable-unit/packet?scope_id=scope-1&generation_id=generation-1",
				Message: deployableUnitPacketReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &EvidenceHandler{AdmissionDecisions: erroringAdmissionDecisionStore{err: err}, Profile: ProfileProduction}
				},
			},
			capability: admissionDecisionCapability,
		},
		{
			route: failure.Route{
				Name: "drift packet", Path: "/api/v0/investigations/drift/packet?scope_id=aws-account-1",
				Message: driftPacketReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &CloudRuntimeDriftHandler{Store: fakeMultiCloudRuntimeDriftStore{listErr: err}}
				},
			},
			capability: cloudRuntimeDriftReadbackCapability,
		},
	}
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer}
	for _, r := range routes {
		suite.Routes = append(suite.Routes, r.route)
	}
	failure.Run(t, suite)
	for _, r := range routes {
		assertFenceCapability(t, suite, r.route, r.capability)
	}
}

// TestLiveEvidenceBundleAnswersFixedServerFailures covers GET
// /api/v0/evidence/bundle, which opens no handler span of its own. Its status
// snapshot read answered 500 with the reader error text. The route has no
// capability-matrix key (#8227), so its fence envelope names none.
func TestLiveEvidenceBundleAnswersFixedServerFailures(t *testing.T) {
	route := failure.Route{
		Name: "live evidence bundle", Path: "/api/v0/evidence/bundle",
		Message: liveEvidenceBundleStatusReadFailedMessage,
		Handler: func(err error) failure.Mounter {
			return &EvidenceHandler{StatusReader: fakeStatusReader{err: err}}
		},
	}
	suite := failure.Suite{Routes: []failure.Route{route}}
	failure.Run(t, suite)
	assertFenceCapability(t, suite, route, "")
}

// failingEvidenceContentStore fails the relationship evidence read, the
// citation file batch, and the entity batch with err.
type failingEvidenceContentStore struct {
	fakePortContentStore
	err error
}

func (s failingEvidenceContentStore) RelationshipEvidenceByResolvedID(
	context.Context, string,
) (relationshipEvidenceReadModel, error) {
	return relationshipEvidenceReadModel{}, s.err
}

func (s failingEvidenceContentStore) EvidenceCitationFiles(
	context.Context, []evidenceCitationFileLookup,
) (map[evidenceCitationFileKey]FileContent, error) {
	return nil, s.err
}

func (s failingEvidenceContentStore) GetEntityContents(context.Context, []string) (map[string]*EntityContent, error) {
	return nil, s.err
}

// erroringAdmissionDecisionStore fails the admission decision list with err.
type erroringAdmissionDecisionStore struct{ err error }

func (s erroringAdmissionDecisionStore) ListAdmissionDecisions(
	context.Context, AdmissionDecisionReadFilter,
) ([]AdmissionDecisionReadRow, error) {
	return nil, s.err
}

func (s erroringAdmissionDecisionStore) ListAdmissionDecisionEvidence(
	context.Context, string, int,
) ([]AdmissionDecisionEvidenceRow, error) {
	return nil, s.err
}
