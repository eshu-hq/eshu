// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// ensureSiblingAdvisoryCapabilities registers the advisory leaf capabilities
// this file's catalog/evidence/detail branches drive. The hub TestMain owns
// only the ten hub capabilities; production registers these two with the same
// AuthoritativeExactSupport shape in go/internal/query/contract/supply_chain.go,
// so this mirrors that row instead of copying its fields.
var ensureSiblingAdvisoryCapabilities = sync.OnceFunc(func() {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{
			Capability: advisory.CatalogCapability,
			Support:    AuthoritativeExactSupport(),
		},
		querycontract.CapabilityRegistration{
			Capability: advisory.EvidenceCapability,
			Support:    AuthoritativeExactSupport(),
		},
	)
})

// Sibling-route doubles. Each is a twin of a staying root-suite double (see
// the hub AGENTS.md twins rule): it fails the one store read its branch
// exercises so the test proves a handler-owned 500 emits the attributable
// signal. They carry no other behavior.

type errExplanationStore struct {
	row impact.ExplanationRow
	err error
}

func (s errExplanationStore) ExplainSupplyChainImpact(
	context.Context,
	impact.ExplanationFilter,
) (impact.ExplanationRow, error) {
	if s.err != nil {
		return impact.ExplanationRow{}, s.err
	}
	return s.row, nil
}

type errImpactAggregateStore struct {
	countErr     error
	inventoryErr error
}

func (s errImpactAggregateStore) CountSupplyChainImpactFindings(
	context.Context,
	impact.AggregateFilter,
) (impact.AggregateCount, error) {
	if s.countErr != nil {
		return impact.AggregateCount{}, s.countErr
	}
	return impact.AggregateCount{}, nil
}

func (s errImpactAggregateStore) SupplyChainImpactInventory(
	context.Context,
	impact.AggregateFilter,
	impact.InventoryDimension,
	int,
	int,
) ([]impact.InventoryRow, error) {
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return nil, nil
}

type errContainerImageStore struct{ err error }

func (s errContainerImageStore) ListContainerImageIdentities(
	context.Context,
	ContainerImageIdentityFilter,
) ([]ContainerImageIdentityRow, error) {
	return nil, s.err
}

type errContainerImageAggregateStore struct {
	countErr     error
	inventoryErr error
}

func (s errContainerImageAggregateStore) CountContainerImageIdentities(
	context.Context,
	ContainerImageIdentityAggregateFilter,
) (ContainerImageIdentityAggregateCount, error) {
	if s.countErr != nil {
		return ContainerImageIdentityAggregateCount{}, s.countErr
	}
	return ContainerImageIdentityAggregateCount{}, nil
}

func (s errContainerImageAggregateStore) ContainerImageIdentityInventory(
	context.Context,
	ContainerImageIdentityAggregateFilter,
	ContainerImageIdentityInventoryDimension,
	int,
	int,
) ([]ContainerImageIdentityInventoryRow, error) {
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return nil, nil
}

type errSBOMAttachmentStore struct{ err error }

func (s errSBOMAttachmentStore) ListSBOMAttestationAttachments(
	context.Context,
	SBOMAttestationAttachmentFilter,
) (SBOMAttestationAttachmentPage, error) {
	return SBOMAttestationAttachmentPage{}, s.err
}

type errSBOMAttachmentAggregateStore struct {
	countErr     error
	inventoryErr error
}

func (s errSBOMAttachmentAggregateStore) CountSBOMAttestationAttachments(
	context.Context,
	SBOMAttestationAttachmentAggregateFilter,
) (SBOMAttestationAttachmentAggregateCount, error) {
	if s.countErr != nil {
		return SBOMAttestationAttachmentAggregateCount{}, s.countErr
	}
	return SBOMAttestationAttachmentAggregateCount{}, nil
}

func (s errSBOMAttachmentAggregateStore) SBOMAttestationAttachmentInventory(
	context.Context,
	SBOMAttestationAttachmentAggregateFilter,
	SBOMAttestationAttachmentInventoryDimension,
	int,
	int,
) ([]SBOMAttestationAttachmentInventoryRow, error) {
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return nil, nil
}

type errSecurityAlertStore struct{ err error }

func (s errSecurityAlertStore) ListSecurityAlertReconciliations(
	context.Context,
	SecurityAlertReconciliationFilter,
) ([]SecurityAlertReconciliationRow, error) {
	return nil, s.err
}

type errSecurityAlertAggregateStore struct {
	countErr     error
	inventoryErr error
}

func (s errSecurityAlertAggregateStore) CountSecurityAlertReconciliations(
	context.Context,
	SecurityAlertReconciliationAggregateFilter,
) (SecurityAlertReconciliationAggregateCount, error) {
	if s.countErr != nil {
		return SecurityAlertReconciliationAggregateCount{}, s.countErr
	}
	return SecurityAlertReconciliationAggregateCount{}, nil
}

func (s errSecurityAlertAggregateStore) SecurityAlertReconciliationInventory(
	context.Context,
	SecurityAlertReconciliationAggregateFilter,
	SecurityAlertReconciliationInventoryDimension,
	int,
	int,
) ([]SecurityAlertReconciliationInventoryRow, error) {
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return nil, nil
}

type errAdvisoryEvidenceStore struct{ err error }

func (s errAdvisoryEvidenceStore) ListAdvisoryEvidence(
	context.Context,
	advisory.EvidenceFilter,
) ([]advisory.EvidenceRow, error) {
	return nil, s.err
}

type errAdvisoryCatalogStore struct{ err error }

func (s errAdvisoryCatalogStore) ListAdvisoryCatalog(
	context.Context,
	advisory.CatalogFilter,
) (advisory.CatalogPage, error) {
	return advisory.CatalogPage{}, s.err
}

// stubImpactPacketResponder is a no-op ImpactPacketResponder so the packet
// branches reach the explanation store read instead of the nil-responder
// 503 guard. The error paths under test return before touching it.
type stubImpactPacketResponder struct{}

func (stubImpactPacketResponder) RespondSupplyChainImpactPacket(
	http.ResponseWriter,
	*http.Request,
	impact.ExplanationResult,
	*querycontract.TruthEnvelope,
) {
}

func (stubImpactPacketResponder) RespondSupplyChainImpactScopeRefusal(
	http.ResponseWriter,
	*http.Request,
) {
}

// siblingProbeExplanationRow is a successful explanation read carrying the
// shared failing-stage digest and repository, so the explain probe branches
// exercise the same probe inputs as the findings route.
func siblingProbeExplanationRow() impact.ExplanationRow {
	return impact.ExplanationRow{Finding: failedStageRow()}
}

func serveSiblingRoute(t *testing.T, handler *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestSiblingRoutesLogFailedStageOnHandlerOwned500 is the #7549 regression
// proof: every sibling supply-chain query route answered a handler-owned HTTP
// 500 with no attributable log line (querycontract.WriteError never logs).
// Each branch below must now emit exactly one ERROR
// supply_chain_query.stage_failed record with its operation, stage, and
// bounded error, and record the error on the handler span with Error status.
func TestSiblingRoutesLogFailedStageOnHandlerOwned500(t *testing.T) {
	ensureSiblingAdvisoryCapabilities()
	// Not parallel: swaps the package-global queryHandlerTracer.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("sibling-stage-failed-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	boom := errors.New("store unavailable")
	branches := []struct {
		name      string
		target    string
		spanName  string
		operation string
		stage     string
		build     func() *Handler
	}{
		{
			name:      "explain read",
			target:    "/api/v0/supply-chain/impact/explain?finding_id=finding-1",
			spanName:  telemetry.SpanQuerySupplyChainImpactExplanation,
			operation: supplyChainImpactExplanationOperation,
			stage:     "impact_explanation_query",
			build: func() *Handler {
				return &Handler{ImpactExplanations: errExplanationStore{err: boom}}
			},
		},
		{
			name:      "explain cloud runtime probe",
			target:    "/api/v0/supply-chain/impact/explain?finding_id=finding-1",
			spanName:  telemetry.SpanQuerySupplyChainImpactExplanation,
			operation: supplyChainImpactExplanationOperation,
			stage:     "cloud_runtime_evidence",
			build: func() *Handler {
				digest := failedStageRow().SubjectDigest
				cloudGraph := &graph.FakeCloudRuntimeGraph{RowsByDigest: map[string][]map[string]any{
					digest: {cloudResourceGraphRow("uid-x", digest, "arn-x")},
				}}
				return &Handler{
					Neo4j:                  cloudGraph,
					ImpactExplanations:     errExplanationStore{row: siblingProbeExplanationRow()},
					CloudResourceInventory: &stubCloudInventory{err: boom, rowsByDigest: cloudGraph.RowsByDigest},
				}
			},
		},
		{
			name:      "explain kubernetes runtime probe",
			target:    "/api/v0/supply-chain/impact/explain?finding_id=finding-1",
			spanName:  telemetry.SpanQuerySupplyChainImpactExplanation,
			operation: supplyChainImpactExplanationOperation,
			stage:     "kubernetes_runtime_evidence",
			build: func() *Handler {
				digest := failedStageRow().SubjectDigest
				return &Handler{
					Neo4j: &graph.FakeKubernetesRuntimeGraph{Rows: []map[string]any{{
						"matched_digest": digest, "workload_uid": "kw-1", "edge_scope_id": "scope-1", "edge_generation_id": "gen-1",
					}}},
					ImpactExplanations:          errExplanationStore{row: siblingProbeExplanationRow()},
					KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{err: boom},
				}
			},
		},
		{
			name:      "explain runtime context probe",
			target:    "/api/v0/supply-chain/impact/explain?finding_id=finding-1",
			spanName:  telemetry.SpanQuerySupplyChainImpactExplanation,
			operation: supplyChainImpactExplanationOperation,
			stage:     "runtime_context",
			build: func() *Handler {
				return &Handler{
					ImpactExplanations: errExplanationStore{row: siblingProbeExplanationRow()},
					ImpactFindings: &graph.FakeRuntimeContextFindingStore{
						Rows: []impact.FindingRow{failedStageRow()},
						Err:  boom,
					},
				}
			},
		},
		{
			name:      "impact packet read",
			target:    "/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1",
			spanName:  telemetry.SpanQuerySupplyChainImpactExplanation,
			operation: supplyChainImpactPacketOperation,
			stage:     "impact_explanation_query",
			build: func() *Handler {
				return &Handler{ImpactExplanations: errExplanationStore{err: boom}, PacketResponder: stubImpactPacketResponder{}}
			},
		},
		{
			name:      "impact aggregate count",
			target:    "/api/v0/supply-chain/impact/findings/count",
			spanName:  telemetry.SpanQuerySupplyChainImpactAggregate,
			operation: supplyChainImpactAggregateOperation,
			stage:     "impact_aggregate_count",
			build: func() *Handler {
				return &Handler{ImpactAggregates: errImpactAggregateStore{countErr: boom}}
			},
		},
		{
			name:      "impact aggregate inventory",
			target:    "/api/v0/supply-chain/impact/inventory?group_by=impact_status&limit=10",
			spanName:  telemetry.SpanQuerySupplyChainImpactAggregate,
			operation: supplyChainImpactAggregateOperation,
			stage:     "impact_aggregate_inventory",
			build: func() *Handler {
				return &Handler{ImpactAggregates: errImpactAggregateStore{inventoryErr: boom}}
			},
		},
		{
			name:      "container image aggregate count",
			target:    "/api/v0/supply-chain/container-images/identities/count",
			spanName:  telemetry.SpanQueryContainerImageIdentityAggregate,
			operation: supplyChainContainerImageAggregateOperation,
			stage:     "container_image_aggregate_count",
			build: func() *Handler {
				return &Handler{ContainerImageAggregates: errContainerImageAggregateStore{countErr: boom}}
			},
		},
		{
			name:      "container image aggregate inventory",
			target:    "/api/v0/supply-chain/container-images/identities/inventory?group_by=outcome&limit=10",
			spanName:  telemetry.SpanQueryContainerImageIdentityAggregate,
			operation: supplyChainContainerImageAggregateOperation,
			stage:     "container_image_aggregate_inventory",
			build: func() *Handler {
				return &Handler{ContainerImageAggregates: errContainerImageAggregateStore{inventoryErr: boom}}
			},
		},
		{
			name:      "sbom attachment aggregate count",
			target:    "/api/v0/supply-chain/sbom-attestations/attachments/count",
			spanName:  telemetry.SpanQuerySBOMAttestationAttachmentAggregate,
			operation: supplyChainSBOMAttachmentAggregateOperation,
			stage:     "sbom_attachment_aggregate_count",
			build: func() *Handler {
				return &Handler{SBOMAttachmentAggregates: errSBOMAttachmentAggregateStore{countErr: boom}}
			},
		},
		{
			name:      "sbom attachment aggregate inventory",
			target:    "/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=attachment_status&limit=10",
			spanName:  telemetry.SpanQuerySBOMAttestationAttachmentAggregate,
			operation: supplyChainSBOMAttachmentAggregateOperation,
			stage:     "sbom_attachment_aggregate_inventory",
			build: func() *Handler {
				return &Handler{SBOMAttachmentAggregates: errSBOMAttachmentAggregateStore{inventoryErr: boom}}
			},
		},
		{
			name:      "security alert aggregate count",
			target:    "/api/v0/supply-chain/security-alerts/reconciliations/count?cve_id=CVE-2026-0001",
			spanName:  telemetry.SpanQuerySecurityAlertReconciliationAggregate,
			operation: supplyChainSecurityAlertAggregateOperation,
			stage:     "security_alert_aggregate_count",
			build: func() *Handler {
				return &Handler{SecurityAlertAggregates: errSecurityAlertAggregateStore{countErr: boom}}
			},
		},
		{
			name:      "security alert aggregate inventory",
			target:    "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=reconciliation_status&limit=10",
			spanName:  telemetry.SpanQuerySecurityAlertReconciliationAggregate,
			operation: supplyChainSecurityAlertAggregateOperation,
			stage:     "security_alert_aggregate_inventory",
			build: func() *Handler {
				return &Handler{SecurityAlertAggregates: errSecurityAlertAggregateStore{inventoryErr: boom}}
			},
		},
		{
			name:      "advisory catalog list",
			target:    "/api/v0/supply-chain/advisories?limit=10",
			spanName:  telemetry.SpanQueryAdvisoryCatalog,
			operation: supplyChainAdvisoryCatalogOperation,
			stage:     "advisory_catalog_query",
			build: func() *Handler {
				return &Handler{AdvisoryCatalog: errAdvisoryCatalogStore{err: boom}}
			},
		},
		{
			name:      "advisory evidence list",
			target:    "/api/v0/supply-chain/advisories/evidence?limit=10&cve_id=CVE-2026-0001",
			spanName:  telemetry.SpanQueryAdvisoryEvidence,
			operation: supplyChainAdvisoryEvidenceOperation,
			stage:     "advisory_evidence_query",
			build: func() *Handler {
				return &Handler{AdvisoryEvidence: errAdvisoryEvidenceStore{err: boom}}
			},
		},
		{
			name:      "vulnerability detail",
			target:    "/api/v0/supply-chain/vulnerabilities/CVE-2026-0001",
			spanName:  telemetry.SpanQueryAdvisoryEvidence,
			operation: supplyChainVulnerabilityDetailOperation,
			stage:     "vulnerability_detail_query",
			build: func() *Handler {
				return &Handler{AdvisoryEvidence: errAdvisoryEvidenceStore{err: boom}}
			},
		},
		{
			name:      "container image identities list",
			target:    "/api/v0/supply-chain/container-images/identities?limit=10&digest=sha256:abc",
			spanName:  telemetry.SpanQueryContainerImageIdentities,
			operation: supplyChainContainerImageIdentityOperation,
			stage:     "container_image_identity_query",
			build: func() *Handler {
				return &Handler{ContainerImageIdentities: errContainerImageStore{err: boom}}
			},
		},
		{
			name:      "sbom attachments list",
			target:    "/api/v0/supply-chain/sbom-attestations/attachments?limit=10&subject_digest=sha256:abc",
			spanName:  telemetry.SpanQuerySBOMAttestationAttachments,
			operation: supplyChainSBOMAttachmentOperation,
			stage:     "sbom_attachment_query",
			build: func() *Handler {
				return &Handler{SBOMAttachments: errSBOMAttachmentStore{err: boom}}
			},
		},
		{
			name:      "security alert reconciliations list",
			target:    "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001",
			spanName:  telemetry.SpanQuerySupplyChainSecurityAlerts,
			operation: supplyChainSecurityAlertReconciliationOperation,
			stage:     "security_alert_reconciliation_query",
			build: func() *Handler {
				return &Handler{SecurityAlerts: errSecurityAlertStore{err: boom}}
			},
		},
	}
	for _, branch := range branches {
		t.Run(branch.name, func(t *testing.T) {
			spansBefore := len(recorder.Ended())
			var logBuf bytes.Buffer
			handler := branch.build()
			handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))

			rec := serveSiblingRoute(t, handler, branch.target)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
			}
			failed := recordsWithEvent(decodeLogRecords(t, &logBuf), "supply_chain_query.stage_failed")
			if len(failed) != 1 {
				t.Fatalf("stage_failed records = %d, want 1; log=%s", len(failed), logBuf.String())
			}
			record := failed[0]
			if record["level"] != "ERROR" {
				t.Fatalf("stage_failed level = %v, want ERROR", record["level"])
			}
			if record["stage"] != branch.stage || record["operation"] != branch.operation {
				t.Fatalf("stage_failed identity = %#v, want stage=%q operation=%q",
					record, branch.stage, branch.operation)
			}
			if record["error_site"] != "other" || record["error_cause"] != "unknown" {
				t.Fatalf("stage_failed error_site/error_cause = %v/%v, want other/unknown",
					record["error_site"], record["error_cause"])
			}

			var handlerSpan sdktrace.ReadOnlySpan
			for _, span := range recorder.Ended()[spansBefore:] {
				if span.Name() == branch.spanName {
					handlerSpan = span
				}
			}
			if handlerSpan == nil {
				t.Fatalf("no ended %s span", branch.spanName)
			}
			if handlerSpan.Status().Code != codes.Error {
				t.Fatalf("handler span status = %v, want Error", handlerSpan.Status().Code)
			}
			hasException := false
			for _, event := range handlerSpan.Events() {
				if event.Name == "exception" {
					hasException = true
				}
			}
			if !hasException {
				t.Fatalf("handler span has no recorded exception event; events=%#v", handlerSpan.Events())
			}
		})
	}
}
