// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	iacFailureAccountBody = `{"account_id":"123456789012"}`
	iacFailureExactBody   = `{"account_id":"123456789012","arn":"arn:aws:s3:::orders"}`
	iacFailurePlanBody    = `{"scope_kind":"account","account_id":"123456789012"}`
	iacFailureDeadBody    = `{"repo_id":"repo://acme/infra"}`
)

// managementCountFails and managementListFails build a handler whose
// management count or list read fails with err.
func managementCountFails(err error) *Handler {
	return &Handler{Management: failingManagementStore{countErr: err}}
}

func managementListFails(err error) *Handler {
	return &Handler{Management: failingManagementStore{listErr: err}}
}

// managementCountListRoutes returns the count and list failure sites of one
// route that reads the management store twice.
func managementCountListRoutes(name, path, body, countMessage, listMessage string) []iacFailureRoute {
	return []iacFailureRoute{
		{name: name + " count", method: http.MethodPost, path: path, body: body, message: countMessage, handler: managementCountFails},
		{name: name + " list", method: http.MethodPost, path: path, body: body, message: listMessage, handler: managementListFails},
	}
}

// TestIaCManagementRoutesAnswerFixedServerFailures is the #7674 regression for
// every route that reads the IaC management store. Each answered 500 with the
// store's error text, skipped the reader-fence 503, and answered a client
// cancel as a server fault. Not parallel: it swaps iacHandlerTracer.
func TestIaCManagementRoutesAnswerFixedServerFailures(t *testing.T) {
	var routes []iacFailureRoute
	routes = append(routes, managementCountListRoutes("unmanaged resources", "/api/v0/iac/unmanaged-resources",
		iacFailureAccountBody, unmanagedResourcesCountFailedMessage, unmanagedResourcesListFailedMessage)...)
	routes = append(routes, managementCountListRoutes("terraform import plan", "/api/v0/iac/terraform-import-plan/candidates",
		iacFailureAccountBody, terraformImportPlanCountFailedMessage, terraformImportPlanListFailedMessage)...)
	routes = append(routes, managementCountListRoutes("aws runtime drift", "/api/v0/aws/runtime-drift/findings",
		iacFailureAccountBody, awsRuntimeDriftCountFailedMessage, awsRuntimeDriftListFailedMessage)...)
	routes = append(routes, managementCountListRoutes("replatforming rollups", "/api/v0/replatforming/rollups",
		iacFailureAccountBody, replatformingRollupsCountFailedMessage, replatformingRollupsListFailedMessage)...)
	routes = append(routes, managementCountListRoutes("replatforming ownership", "/api/v0/replatforming/ownership-packets",
		iacFailureAccountBody, replatformingOwnershipCountFailedMessage, replatformingOwnershipListFailedMessage)...)
	routes = append(routes, managementCountListRoutes("replatforming plan", ReplatformingPlanRoute,
		iacFailurePlanBody, replatformingPlanCountFailedMessage, replatformingPlanListFailedMessage)...)
	routes = append(routes, managementCountListRoutes("management status", "/api/v0/iac/management-status",
		iacFailureExactBody, managementStatusReadFailedMessage, managementStatusReadFailedMessage)...)
	routes = append(routes, managementCountListRoutes("management explanation", "/api/v0/iac/management-status/explain",
		iacFailureExactBody, managementExplanationReadFailedMessage, managementExplanationReadFailedMessage)...)
	routes = append(routes, iacFailureRoute{
		name: "replatforming selectors", method: http.MethodGet, path: "/api/v0/replatforming/selectors",
		message: replatformingSelectorsFailedMessage,
		handler: func(err error) *Handler { return &Handler{Management: failingManagementStore{selectorErr: err}} },
	})
	runIACFailureRoutes(t, routes)
}

// TestIaCResourcesAnswerFixedServerFailures covers the inventory search, the
// graph hydration, and the facet summary reads of GET /api/v0/iac/resources.
func TestIaCResourcesAnswerFixedServerFailures(t *testing.T) {
	candidate := InventoryCandidate{ID: "content-entity:e_1", Name: "aws_s3_bucket.logs", GenerationID: "generation-active"}
	hydrated := func() []map[string]any {
		return []map[string]any{iacResourceRepoNode(candidate.ID, candidate.Name, "aws_s3_bucket", "aws", "repository:r_active")}
	}
	runIACFailureRoutes(t, []iacFailureRoute{
		{
			name: "inventory search", method: http.MethodGet, path: "/api/v0/iac/resources?limit=5",
			message: iacResourcesSearchFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Graph: &stubIaCResourceGraph{}, Inventory: failingInventoryStore{searchErr: err}}
			},
		},
		{
			name: "graph hydration", method: http.MethodGet, path: "/api/v0/iac/resources?limit=5",
			message: iacResourcesGraphFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{
					Graph:     &stubIaCResourceGraph{err: err},
					Inventory: failingInventoryStore{candidates: []InventoryCandidate{candidate}},
				}
			},
		},
		{
			name: "facet summary", method: http.MethodGet, path: "/api/v0/iac/resources?limit=5&include_facets=true",
			message: iacResourcesSummaryFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{
					Graph:     &stubIaCResourceGraph{rows: hydrated()},
					Inventory: failingInventoryStore{candidates: []InventoryCandidate{candidate}, summaryErr: err},
				}
			},
		},
	})
}

// TestDeadIaCAnswersFixedServerFailures covers each dead-IaC read step. The
// reader fence 503 was already answered (#7523); the 500 carried the store's
// error text and a client cancel read as a server fault.
func TestDeadIaCAnswersFixedServerFailures(t *testing.T) {
	reachability := func(site string) func(error) *Handler {
		return func(err error) *Handler {
			return &Handler{Reachability: readerFenceReachability{site: site, err: err}}
		}
	}
	runIACFailureRoutes(t, []iacFailureRoute{
		{
			name: "count", method: http.MethodPost, path: "/api/v0/iac/dead", body: iacFailureDeadBody,
			message: deadIaCCountFailedMessage, handler: reachability("count"),
		},
		{
			name: "list", method: http.MethodPost, path: "/api/v0/iac/dead", body: iacFailureDeadBody,
			message: deadIaCListFailedMessage, handler: reachability("list"),
		},
		{
			name: "has rows", method: http.MethodPost, path: "/api/v0/iac/dead", body: iacFailureDeadBody,
			message: deadIaCRowsCheckFailedMessage, handler: reachability("has_rows"),
		},
		{
			name: "content files", method: http.MethodPost, path: "/api/v0/iac/dead", body: iacFailureDeadBody,
			message: deadIaCFilesFailedMessage, handler: func(err error) *Handler {
				return &Handler{Content: readerFenceContent{filesErr: err}}
			},
		},
	})
}

// TestReplatformingPlanValidationFailureAnswersFixedMessage pins the composed
// plan contract check: two findings sharing one id fail validation, whose
// error quotes that id. The body is fixed text and the span records the cause.
func TestReplatformingPlanValidationFailureAnswersFixedMessage(t *testing.T) {
	finding := ManagementFindingRow{
		ID: "finding:" + iacFailureCanary, ARN: "arn:aws:s3:::orders", ScopeID: "aws:123456789012:us-east-1:s3",
		AccountID: "123456789012", Region: "us-east-1", ResourceType: "s3_bucket",
		FindingKind: FindingKindUnmanagedCloudResource, ManagementStatus: "unmanaged",
	}
	handler := &Handler{Management: failingManagementStore{rows: []ManagementFindingRow{finding, finding}}}
	rec, ended := serveIACTraced(t, handler, http.MethodPost, ReplatformingPlanRoute, iacFailurePlanBody, false)
	assertIACFailure(t, replatformingPlanValidationFailedMessage, iacFailureCases()[0], rec, ended)
}

// TestIaCResourcesMismatchMarksSpanError pins the inventory/graph exactness
// failure: it keeps its fixed 500 body and now records the fault on the span.
func TestIaCResourcesMismatchMarksSpanError(t *testing.T) {
	handler := &Handler{
		Graph: &stubIaCResourceGraph{},
		Inventory: failingInventoryStore{candidates: []InventoryCandidate{
			{ID: "content-entity:e_missing", Name: "aws_s3_bucket.missing", GenerationID: "generation-active"},
		}},
	}
	rec, ended := serveIACTraced(t, handler, http.MethodGet, "/api/v0/iac/resources?q=missing&limit=10", "", false)
	if rec.Code != http.StatusInternalServerError || iacFailureMessage(rec.Body.Bytes()) != iacResourcesMismatchMessage {
		t.Fatalf("status = %d, body = %s; want 500 with %q", rec.Code, rec.Body.String(), iacResourcesMismatchMessage)
	}
	assertIACFailureSpan(t, iacResourcesMismatchMessage, false, ended)
}

// TestReplatformingSelectorsFailureKeepsContractEnvelope pins wire parity for
// the selector inventory 500: the envelope keeps internal_error, the
// capability, and both profiles. It passed before #7674 and must keep passing.
func TestReplatformingSelectorsFailureKeepsContractEnvelope(t *testing.T) {
	handler := &Handler{Management: failingManagementStore{selectorErr: iacFailureCases()[0].err}}
	rec, _ := serveIACTraced(t, handler, http.MethodGet, "/api/v0/replatforming/selectors", "", false)
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil {
		t.Fatalf("body = %s (decode err %v), want an error envelope", rec.Body.String(), err)
	}
	got := envelope.Error
	if got.Code != querycontract.ErrorCodeInternalError || got.Capability != ReplatformingSelectorInventoryCapability ||
		got.Profiles == nil || got.Profiles.Current != handler.profile() ||
		got.Profiles.Required != querycontract.RequiredProfile(ReplatformingSelectorInventoryCapability) {
		t.Fatalf("error envelope = %+v (profiles %+v), want internal_error with capability and profiles", got, got.Profiles)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("body leaked the backend error text: %s", rec.Body.String())
	}
}
