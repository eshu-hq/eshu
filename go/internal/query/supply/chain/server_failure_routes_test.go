// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	vulnerabilitysuppressionv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerabilitysuppression/v1"
)

const (
	explainTarget = "/api/v0/supply-chain/impact/explain?finding_id=finding-1"
	selectorRoute = "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&repository_id=" + selectorTestSelector
)

// TestImpactFindingsServerFailures is the #7674 proof for every failure step
// of the impact-findings route: a server fault answers its fixed message,
// never the backend text; a client cancel answers 499 with one INFO
// stage_canceled and no stage_failed; an inner-context cancel on a live
// request stays a 500 fault; a reader fence keeps the retryable 503.
func TestImpactFindingsServerFailures(t *testing.T) {
	findings := func(stage, message string, build func(err error) *Handler) supplyFailureRoute {
		return supplyFailureRoute{
			name: stage, target: failedStageRoute, spanName: telemetry.SpanQuerySupplyChainImpactFindings,
			operation: supplyChainImpactFindingsOperation, stage: stage, message: message, build: build,
		}
	}
	runSupplyFailureRoutes(t, []supplyFailureRoute{
		findings("impact_findings_query", "supply-chain impact findings read failed", func(err error) *Handler {
			return &Handler{ImpactFindings: failingImpactFindingsStore{err: err}}
		}),
		findings("cloud_runtime_evidence", "supply-chain impact runtime evidence probe failed", func(err error) *Handler {
			digest := failedStageRow().SubjectDigest
			cloudGraph := &graph.FakeCloudRuntimeGraph{RowsByDigest: map[string][]map[string]any{
				digest: {cloudResourceGraphRow("uid-x", digest, "arn-x")},
			}}
			return &Handler{
				Neo4j:                  cloudGraph,
				ImpactFindings:         &graph.FakeRuntimeContextFindingStore{Rows: []impact.FindingRow{failedStageRow()}},
				CloudResourceInventory: &stubCloudInventory{err: err, rowsByDigest: cloudGraph.RowsByDigest},
			}
		}),
		findings("kubernetes_runtime_evidence", "supply-chain impact kubernetes runtime evidence probe failed", func(err error) *Handler {
			return &Handler{
				Neo4j:                       siblingKubernetesGraph(),
				ImpactFindings:              &graph.FakeRuntimeContextFindingStore{Rows: []impact.FindingRow{failedStageRow()}},
				KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{err: err},
			}
		}),
		findings("runtime_context", "supply-chain impact runtime context probe failed", func(err error) *Handler {
			return &Handler{ImpactFindings: &graph.FakeRuntimeContextFindingStore{
				Rows: []impact.FindingRow{failedStageRow()},
				Err:  err,
			}}
		}),
	})
}

// TestSiblingRoutesServerFailures applies the same four-case contract to
// every sibling supply-chain read step (#7674).
func TestSiblingRoutesServerFailures(t *testing.T) {
	ensureSiblingAdvisoryCapabilities()
	explain := func(stage, message string, build func(err error) *Handler) supplyFailureRoute {
		return supplyFailureRoute{
			name: "explain " + stage, target: explainTarget, spanName: telemetry.SpanQuerySupplyChainImpactExplanation,
			operation: supplyChainImpactExplanationOperation, stage: stage, message: message, build: build,
		}
	}
	runSupplyFailureRoutes(t, []supplyFailureRoute{
		explain("impact_explanation_query", "supply-chain impact explanation read failed", func(err error) *Handler {
			return &Handler{ImpactExplanations: errExplanationStore{err: err}}
		}),
		explain("cloud_runtime_evidence", "supply-chain impact runtime evidence probe failed", func(err error) *Handler {
			digest := failedStageRow().SubjectDigest
			cloudGraph := &graph.FakeCloudRuntimeGraph{RowsByDigest: map[string][]map[string]any{
				digest: {cloudResourceGraphRow("uid-x", digest, "arn-x")},
			}}
			return &Handler{
				Neo4j:                  cloudGraph,
				ImpactExplanations:     errExplanationStore{row: siblingProbeExplanationRow()},
				CloudResourceInventory: &stubCloudInventory{err: err, rowsByDigest: cloudGraph.RowsByDigest},
			}
		}),
		explain("kubernetes_runtime_evidence", "supply-chain impact kubernetes runtime evidence probe failed", func(err error) *Handler {
			return &Handler{
				Neo4j:                       siblingKubernetesGraph(),
				ImpactExplanations:          errExplanationStore{row: siblingProbeExplanationRow()},
				KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{err: err},
			}
		}),
		explain("runtime_context", "supply-chain impact runtime context probe failed", func(err error) *Handler {
			return &Handler{
				ImpactExplanations: errExplanationStore{row: siblingProbeExplanationRow()},
				ImpactFindings:     &graph.FakeRuntimeContextFindingStore{Rows: []impact.FindingRow{failedStageRow()}, Err: err},
			}
		}),
		{
			name: "impact packet read", target: "/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1",
			spanName: telemetry.SpanQuerySupplyChainImpactExplanation, operation: supplyChainImpactPacketOperation,
			stage: "impact_explanation_query", message: "supply-chain impact packet read failed",
			build: func(err error) *Handler {
				return &Handler{ImpactExplanations: errExplanationStore{err: err}, PacketResponder: stubImpactPacketResponder{}}
			},
		},
		{
			name: "impact aggregate count", target: "/api/v0/supply-chain/impact/findings/count",
			spanName: telemetry.SpanQuerySupplyChainImpactAggregate, operation: supplyChainImpactAggregateOperation,
			stage: "impact_aggregate_count", message: "supply-chain impact finding count read failed",
			build: func(err error) *Handler { return &Handler{ImpactAggregates: errImpactAggregateStore{countErr: err}} },
		},
		{
			name: "impact aggregate inventory", target: "/api/v0/supply-chain/impact/inventory?group_by=impact_status&limit=10",
			spanName: telemetry.SpanQuerySupplyChainImpactAggregate, operation: supplyChainImpactAggregateOperation,
			stage: "impact_aggregate_inventory", message: "supply-chain impact inventory read failed",
			build: func(err error) *Handler {
				return &Handler{ImpactAggregates: errImpactAggregateStore{inventoryErr: err}}
			},
		},
		{
			name: "container image aggregate count", target: "/api/v0/supply-chain/container-images/identities/count",
			spanName: telemetry.SpanQueryContainerImageIdentityAggregate, operation: supplyChainContainerImageAggregateOperation,
			stage: "container_image_aggregate_count", message: "container image identity count read failed",
			build: func(err error) *Handler {
				return &Handler{ContainerImageAggregates: errContainerImageAggregateStore{countErr: err}}
			},
		},
		{
			name: "container image aggregate inventory", target: "/api/v0/supply-chain/container-images/identities/inventory?group_by=outcome&limit=10",
			spanName: telemetry.SpanQueryContainerImageIdentityAggregate, operation: supplyChainContainerImageAggregateOperation,
			stage: "container_image_aggregate_inventory", message: "container image identity inventory read failed",
			build: func(err error) *Handler {
				return &Handler{ContainerImageAggregates: errContainerImageAggregateStore{inventoryErr: err}}
			},
		},
		{
			name: "sbom attachment aggregate count", target: "/api/v0/supply-chain/sbom-attestations/attachments/count",
			spanName: telemetry.SpanQuerySBOMAttestationAttachmentAggregate, operation: supplyChainSBOMAttachmentAggregateOperation,
			stage: "sbom_attachment_aggregate_count", message: "SBOM attestation attachment count read failed",
			build: func(err error) *Handler {
				return &Handler{SBOMAttachmentAggregates: errSBOMAttachmentAggregateStore{countErr: err}}
			},
		},
		{
			name: "sbom attachment aggregate inventory", target: "/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=attachment_status&limit=10",
			spanName: telemetry.SpanQuerySBOMAttestationAttachmentAggregate, operation: supplyChainSBOMAttachmentAggregateOperation,
			stage: "sbom_attachment_aggregate_inventory", message: "SBOM attestation attachment inventory read failed",
			build: func(err error) *Handler {
				return &Handler{SBOMAttachmentAggregates: errSBOMAttachmentAggregateStore{inventoryErr: err}}
			},
		},
		{
			name: "security alert aggregate count", target: "/api/v0/supply-chain/security-alerts/reconciliations/count?cve_id=CVE-2026-0001",
			spanName: telemetry.SpanQuerySecurityAlertReconciliationAggregate, operation: supplyChainSecurityAlertAggregateOperation,
			stage: "security_alert_aggregate_count", message: "security alert reconciliation count read failed",
			build: func(err error) *Handler {
				return &Handler{SecurityAlertAggregates: errSecurityAlertAggregateStore{countErr: err}}
			},
		},
		{
			name: "security alert aggregate inventory", target: "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=reconciliation_status&limit=10",
			spanName: telemetry.SpanQuerySecurityAlertReconciliationAggregate, operation: supplyChainSecurityAlertAggregateOperation,
			stage: "security_alert_aggregate_inventory", message: "security alert reconciliation inventory read failed",
			build: func(err error) *Handler {
				return &Handler{SecurityAlertAggregates: errSecurityAlertAggregateStore{inventoryErr: err}}
			},
		},
		{
			name: "advisory catalog list", target: "/api/v0/supply-chain/advisories?limit=10",
			spanName: telemetry.SpanQueryAdvisoryCatalog, operation: supplyChainAdvisoryCatalogOperation,
			stage: "advisory_catalog_query", message: "advisory catalog read failed",
			build: func(err error) *Handler { return &Handler{AdvisoryCatalog: errAdvisoryCatalogStore{err: err}} },
		},
		{
			name: "advisory evidence list", target: "/api/v0/supply-chain/advisories/evidence?limit=10&cve_id=CVE-2026-0001",
			spanName: telemetry.SpanQueryAdvisoryEvidence, operation: supplyChainAdvisoryEvidenceOperation,
			stage: "advisory_evidence_query", message: "advisory evidence read failed",
			build: func(err error) *Handler { return &Handler{AdvisoryEvidence: errAdvisoryEvidenceStore{err: err}} },
		},
		{
			name: "vulnerability detail", target: "/api/v0/supply-chain/vulnerabilities/CVE-2026-0001",
			spanName: telemetry.SpanQueryAdvisoryEvidence, operation: supplyChainVulnerabilityDetailOperation,
			stage: "vulnerability_detail_query", message: "vulnerability detail read failed",
			build: func(err error) *Handler { return &Handler{AdvisoryEvidence: errAdvisoryEvidenceStore{err: err}} },
		},
		{
			name: "container image identities list", target: "/api/v0/supply-chain/container-images/identities?limit=10&digest=sha256:abc",
			spanName: telemetry.SpanQueryContainerImageIdentities, operation: supplyChainContainerImageIdentityOperation,
			stage: "container_image_identity_query", message: "container image identity read failed",
			build: func(err error) *Handler { return &Handler{ContainerImageIdentities: errContainerImageStore{err: err}} },
		},
		{
			name: "sbom attachments list", target: "/api/v0/supply-chain/sbom-attestations/attachments?limit=10&subject_digest=sha256:abc",
			spanName: telemetry.SpanQuerySBOMAttestationAttachments, operation: supplyChainSBOMAttachmentOperation,
			stage: "sbom_attachment_query", message: "SBOM attestation attachment read failed",
			build: func(err error) *Handler { return &Handler{SBOMAttachments: errSBOMAttachmentStore{err: err}} },
		},
		{
			name: "security alert reconciliations list", target: "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001",
			spanName: telemetry.SpanQuerySupplyChainSecurityAlerts, operation: supplyChainSecurityAlertReconciliationOperation,
			stage: "security_alert_reconciliation_query", message: "security alert reconciliation read failed",
			build: func(err error) *Handler { return &Handler{SecurityAlerts: errSecurityAlertStore{err: err}} },
		},
	})
}

// TestRepositorySelectorServerFailures applies the four-case contract to the
// three security-alert repository selector reads (#7567, #7626, #7674). Each
// keeps the fixed "repository selector lookup failed" message.
func TestRepositorySelectorServerFailures(t *testing.T) {
	selectorStep := func(stage string, build func(err error) *Handler) supplyFailureRoute {
		return supplyFailureRoute{
			name: stage, target: selectorRoute, spanName: telemetry.SpanQuerySupplyChainSecurityAlerts,
			operation: supplyChainSecurityAlertReconciliationOperation, stage: stage,
			message: "repository selector lookup failed", build: build,
		}
	}
	routes := []supplyFailureRoute{
		selectorStep(selectorResolveStage, func(err error) *Handler {
			return selectorResolveHandler(selectorResolveGraph(nil, err))
		}),
	}
	for _, read := range selectorTestReads() {
		routes = append(routes, selectorStep(read.stage, read.build))
	}
	runSupplyFailureRoutes(t, routes)
}

// TestSuppressionMutationServerFailures pins the suppression write's 500: a
// fixed message, the span marked once, and the same client-cancel handling as
// the read routes. It is a writer path with no stage timer and no reader
// fence, so a stale-reader error is an ordinary fault.
func TestSuppressionMutationServerFailures(t *testing.T) {
	runSupplyFailureRoutes(t, []supplyFailureRoute{{
		name:      "persist suppression",
		method:    "POST",
		writePath: true,
		target:    "/api/v0/supply-chain/impact/suppressions",
		body: `{"suppression_id":"suppression-1","justification":"accepted_risk",` +
			`"authored_at":"2026-07-27T12:00:00Z","expires_at":"2026-08-01T12:00:00Z",` +
			`"reason":"compensating control verified","scope":{"cve_id":"CVE-2026-00001"}}`,
		authCtx:  &auth.AuthContext{Mode: auth.AuthModeShared, SubjectClass: "shared_token", SubjectIDHash: "sha256:operator", AllScopes: true},
		spanName: telemetry.SpanQueryVulnerabilitySuppressionMutation,
		message:  "persist vulnerability suppression",
		build: func(err error) *Handler {
			return &Handler{SuppressionMutations: failingSuppressionStore{err: err}}
		},
	}})
}

// siblingKubernetesGraph answers the RUNS_IMAGE probe with one candidate for
// the shared failing-stage digest, so the owner-ledger read runs next.
func siblingKubernetesGraph() *graph.FakeKubernetesRuntimeGraph {
	return &graph.FakeKubernetesRuntimeGraph{Rows: []map[string]any{{
		"matched_digest": failedStageRow().SubjectDigest, "workload_uid": "kw-1",
		"edge_scope_id": "scope-1", "edge_generation_id": "gen-1",
	}}}
}

// failingSuppressionStore fails every suppression upsert with err.
type failingSuppressionStore struct{ err error }

func (s failingSuppressionStore) UpsertVulnerabilitySuppression(
	context.Context,
	vulnerabilitysuppressionv1.Suppression,
) (impact.VulnerabilitySuppressionMutationResult, error) {
	return impact.VulnerabilitySuppressionMutationResult{}, s.err
}
