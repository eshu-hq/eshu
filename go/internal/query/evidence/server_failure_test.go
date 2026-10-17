// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// statusFailureRoute is one converted evidence-route failure site plus the
// capability its reader-fence envelope must name. It mirrors the root helper
// of the same name in status_server_failure_test.go: the staying root
// failure tests and the moved leaf tests cannot share an unexported helper
// across the package boundary, so each side keeps its own copy.
type statusFailureRoute struct {
	route      failure.Route
	capability string
}

// assertFenceCapability serves route with a stale-reader error and checks the
// envelope's capability field. See the root helper of the same name.
func assertFenceCapability(t *testing.T, suite failure.Suite, route failure.Route, capability string) {
	t.Helper()

	rec, _ := failure.Serve(suite, route, route.Handler(fmt.Errorf("%s: %w", failure.Canary, db.ErrReaderStale)), false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("%s / fence capability: status = %d, want 503; body = %s", route.Name, rec.Code, rec.Body.String())
		return
	}
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Errorf("%s / fence capability: decode body: %v; body = %s", route.Name, err, rec.Body.String())
		return
	}
	if got := body.Error["capability"]; got != capability {
		t.Errorf("%s / fence capability: envelope capability = %v, want %q", route.Name, got, capability)
	}
}

// TestEvidenceRoutesAnswerFixedServerFailures is the #7674 slice 9 regression
// for the evidence, admission decision, and investigation packet reads that
// open a handler span. Each answered 500 with the store error text
// (err.Error()), answered a stale reader with that 500 instead of the
// retryable 503, and answered a client cancel as a server fault. Every store
// here reads through the guarded PostgreSQL reader. Not parallel: it swaps
// queryHandlerTracer.
//
// The deployable-unit packet case wires the stub responder so the proof
// reaches the admission decision store: with a nil responder the route
// answers the unwired-seam 503 before reading (see
// deployable_unit_packet_test.go), which would mask the read-failure body
// proved here.
func TestEvidenceRoutesAnswerFixedServerFailures(t *testing.T) {
	const citationsPath = "/api/v0/evidence/citations"
	routes := []statusFailureRoute{
		{
			route: failure.Route{
				Name: "relationship evidence", Path: "/api/v0/evidence/relationships/resolved-1",
				Message: relationshipEvidenceReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &Handler{Content: failingEvidenceContentStore{err: err}}
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
					return &Handler{Content: failingEvidenceContentStore{err: err}}
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
					return &Handler{Content: failingEvidenceContentStore{err: err}}
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
					return &Handler{
						AdmissionDecisions: erroringAdmissionDecisionStore{err: err},
						Profile:            querycontract.ProfileProduction,
						PacketResponder:    &stubDeployableUnitPacketResponder{},
					}
				},
			},
			capability: AdmissionDecisionCapability,
		},
		{
			route: failure.Route{
				Name: "admission decisions",
				Path: "/api/v0/evidence/admission-decisions?domain=cloud_inventory" +
					"&scope_id=git-repository-scope:team/api&generation_id=generation-1&limit=10",
				Message: admissionDecisionReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &Handler{
						AdmissionDecisions: erroringAdmissionDecisionStore{err: err},
						Profile:            querycontract.ProfileProduction,
					}
				},
			},
			capability: AdmissionDecisionCapability,
		},
		{
			route: failure.Route{
				Name: "admission decision evidence",
				Path: "/api/v0/evidence/admission-decisions?domain=cloud_inventory" +
					"&scope_id=git-repository-scope:team/api&generation_id=generation-1&limit=10&include_evidence=true",
				Message: admissionDecisionEvidenceReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &Handler{
						AdmissionDecisions: evidenceFailingAdmissionDecisionStore{err: err},
						Profile:            querycontract.ProfileProduction,
					}
				},
			},
			capability: AdmissionDecisionCapability,
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
// snapshot read answered 500 with the reader error text. Its fence envelope
// names the route's capability-matrix key (#8227).
func TestLiveEvidenceBundleAnswersFixedServerFailures(t *testing.T) {
	route := failure.Route{
		Name: "live evidence bundle", Path: "/api/v0/evidence/bundle",
		Message: liveEvidenceBundleStatusReadFailedMessage,
		Handler: func(err error) failure.Mounter {
			return &Handler{StatusReader: testutil.FakeStatusReader{Err: err}}
		},
	}
	suite := failure.Suite{Routes: []failure.Route{route}}
	failure.Run(t, suite)
	assertFenceCapability(t, suite, route, "operations.evidence_bundle_live")
}

// failingEvidenceContentStore fails the relationship evidence read, the
// citation file batch, and the entity batch with err. It embeds the content
// store port so the handler's Content field accepts it; only the exercised
// reads are overridden.
type failingEvidenceContentStore struct {
	querycontract.ContentStore
	err error
}

func (s failingEvidenceContentStore) RelationshipEvidenceByResolvedID(
	context.Context, string,
) (RelationshipEvidenceReadModel, error) {
	return RelationshipEvidenceReadModel{}, s.err
}

func (s failingEvidenceContentStore) EvidenceCitationFiles(
	context.Context, []EvidenceCitationFileLookup,
) (map[EvidenceCitationFileKey]querycontract.FileContent, error) {
	return nil, s.err
}

func (s failingEvidenceContentStore) GetEntityContents(context.Context, []string) (map[string]*querycontract.EntityContent, error) {
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

// evidenceFailingAdmissionDecisionStore answers one decision and fails its
// evidence read with err.
type evidenceFailingAdmissionDecisionStore struct{ err error }

func (s evidenceFailingAdmissionDecisionStore) ListAdmissionDecisions(
	context.Context, AdmissionDecisionReadFilter,
) ([]AdmissionDecisionReadRow, error) {
	return []AdmissionDecisionReadRow{{DecisionID: "decision-1"}}, nil
}

func (s evidenceFailingAdmissionDecisionStore) ListAdmissionDecisionEvidence(
	context.Context, string, int,
) ([]AdmissionDecisionEvidenceRow, error) {
	return nil, s.err
}
