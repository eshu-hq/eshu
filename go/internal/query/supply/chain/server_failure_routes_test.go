// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
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
	findings := func(stage, message string, build func(err error) *Handler) failure.Route {
		return stagedSupplyRoute(t, stage, "", failedStageRoute, "", message,
			supplyChainImpactFindingsOperation, stage, telemetry.SpanQuerySupplyChainImpactFindings, build)
	}
	runSupplyFailureRoutes(t, []failure.Route{
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
	explain := func(stage, message string, build func(err error) *Handler) failure.Route {
		return stagedSupplyRoute(t, "explain "+stage, "", explainTarget, "", message,
			supplyChainImpactExplanationOperation, stage, telemetry.SpanQuerySupplyChainImpactExplanation, build)
	}
	runSupplyFailureRoutes(t, []failure.Route{
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
		stagedSupplyRoute(t, "impact packet read", "", "/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1", "",
			"supply-chain impact packet read failed", supplyChainImpactPacketOperation, "impact_explanation_query", telemetry.SpanQuerySupplyChainImpactExplanation,
			func(err error) *Handler {
				return &Handler{ImpactExplanations: errExplanationStore{err: err}, PacketResponder: stubImpactPacketResponder{}}
			}),
		stagedSupplyRoute(t, "impact aggregate count", "", "/api/v0/supply-chain/impact/findings/count", "",
			"supply-chain impact finding count read failed", supplyChainImpactAggregateOperation, "impact_aggregate_count", telemetry.SpanQuerySupplyChainImpactAggregate,
			func(err error) *Handler { return &Handler{ImpactAggregates: errImpactAggregateStore{countErr: err}} }),
		stagedSupplyRoute(t, "impact aggregate inventory", "", "/api/v0/supply-chain/impact/inventory?group_by=impact_status&limit=10", "",
			"supply-chain impact inventory read failed", supplyChainImpactAggregateOperation, "impact_aggregate_inventory", telemetry.SpanQuerySupplyChainImpactAggregate,
			func(err error) *Handler {
				return &Handler{ImpactAggregates: errImpactAggregateStore{inventoryErr: err}}
			}),
		stagedSupplyRoute(t, "container image aggregate count", "", "/api/v0/supply-chain/container-images/identities/count", "",
			"container image identity count read failed", supplyChainContainerImageAggregateOperation, "container_image_aggregate_count", telemetry.SpanQueryContainerImageIdentityAggregate,
			func(err error) *Handler {
				return &Handler{ContainerImageAggregates: errContainerImageAggregateStore{countErr: err}}
			}),
		stagedSupplyRoute(t, "container image aggregate inventory", "", "/api/v0/supply-chain/container-images/identities/inventory?group_by=outcome&limit=10", "",
			"container image identity inventory read failed", supplyChainContainerImageAggregateOperation, "container_image_aggregate_inventory", telemetry.SpanQueryContainerImageIdentityAggregate,
			func(err error) *Handler {
				return &Handler{ContainerImageAggregates: errContainerImageAggregateStore{inventoryErr: err}}
			}),
		stagedSupplyRoute(t, "sbom attachment aggregate count", "", "/api/v0/supply-chain/sbom-attestations/attachments/count", "",
			"SBOM attestation attachment count read failed", supplyChainSBOMAttachmentAggregateOperation, "sbom_attachment_aggregate_count", telemetry.SpanQuerySBOMAttestationAttachmentAggregate,
			func(err error) *Handler {
				return &Handler{SBOMAttachmentAggregates: errSBOMAttachmentAggregateStore{countErr: err}}
			}),
		stagedSupplyRoute(t, "sbom attachment aggregate inventory", "", "/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=attachment_status&limit=10", "",
			"SBOM attestation attachment inventory read failed", supplyChainSBOMAttachmentAggregateOperation, "sbom_attachment_aggregate_inventory", telemetry.SpanQuerySBOMAttestationAttachmentAggregate,
			func(err error) *Handler {
				return &Handler{SBOMAttachmentAggregates: errSBOMAttachmentAggregateStore{inventoryErr: err}}
			}),
		stagedSupplyRoute(t, "security alert aggregate count", "", "/api/v0/supply-chain/security-alerts/reconciliations/count?cve_id=CVE-2026-0001", "",
			"security alert reconciliation count read failed", supplyChainSecurityAlertAggregateOperation, "security_alert_aggregate_count", telemetry.SpanQuerySecurityAlertReconciliationAggregate,
			func(err error) *Handler {
				return &Handler{SecurityAlertAggregates: errSecurityAlertAggregateStore{countErr: err}}
			}),
		stagedSupplyRoute(t, "security alert aggregate inventory", "", "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=reconciliation_status&limit=10", "",
			"security alert reconciliation inventory read failed", supplyChainSecurityAlertAggregateOperation, "security_alert_aggregate_inventory", telemetry.SpanQuerySecurityAlertReconciliationAggregate,
			func(err error) *Handler {
				return &Handler{SecurityAlertAggregates: errSecurityAlertAggregateStore{inventoryErr: err}}
			}),
		stagedSupplyRoute(t, "advisory catalog list", "", "/api/v0/supply-chain/advisories?limit=10", "",
			"advisory catalog read failed", supplyChainAdvisoryCatalogOperation, "advisory_catalog_query", telemetry.SpanQueryAdvisoryCatalog,
			func(err error) *Handler { return &Handler{AdvisoryCatalog: errAdvisoryCatalogStore{err: err}} }),
		stagedSupplyRoute(t, "advisory evidence list", "", "/api/v0/supply-chain/advisories/evidence?limit=10&cve_id=CVE-2026-0001", "",
			"advisory evidence read failed", supplyChainAdvisoryEvidenceOperation, "advisory_evidence_query", telemetry.SpanQueryAdvisoryEvidence,
			func(err error) *Handler { return &Handler{AdvisoryEvidence: errAdvisoryEvidenceStore{err: err}} }),
		stagedSupplyRoute(t, "vulnerability detail", "", "/api/v0/supply-chain/vulnerabilities/CVE-2026-0001", "",
			"vulnerability detail read failed", supplyChainVulnerabilityDetailOperation, "vulnerability_detail_query", telemetry.SpanQueryAdvisoryEvidence,
			func(err error) *Handler { return &Handler{AdvisoryEvidence: errAdvisoryEvidenceStore{err: err}} }),
		stagedSupplyRoute(t, "container image identities list", "", "/api/v0/supply-chain/container-images/identities?limit=10&digest=sha256:abc", "",
			"container image identity read failed", supplyChainContainerImageIdentityOperation, "container_image_identity_query", telemetry.SpanQueryContainerImageIdentities,
			func(err error) *Handler { return &Handler{ContainerImageIdentities: errContainerImageStore{err: err}} }),
		stagedSupplyRoute(t, "sbom attachments list", "", "/api/v0/supply-chain/sbom-attestations/attachments?limit=10&subject_digest=sha256:abc", "",
			"SBOM attestation attachment read failed", supplyChainSBOMAttachmentOperation, "sbom_attachment_query", telemetry.SpanQuerySBOMAttestationAttachments,
			func(err error) *Handler { return &Handler{SBOMAttachments: errSBOMAttachmentStore{err: err}} }),
		stagedSupplyRoute(t, "security alert reconciliations list", "", "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001", "",
			"security alert reconciliation read failed", supplyChainSecurityAlertReconciliationOperation, "security_alert_reconciliation_query", telemetry.SpanQuerySupplyChainSecurityAlerts,
			func(err error) *Handler { return &Handler{SecurityAlerts: errSecurityAlertStore{err: err}} }),
	})
}

// TestRepositorySelectorServerFailures applies the four-case contract to the
// three security-alert repository selector reads (#7567, #7626, #7674). Each
// keeps the fixed "repository selector lookup failed" message.
func TestRepositorySelectorServerFailures(t *testing.T) {
	selectorStep := func(stage string, build func(err error) *Handler) failure.Route {
		return stagedSupplyRoute(t, stage, "", selectorRoute, "", "repository selector lookup failed",
			supplyChainSecurityAlertReconciliationOperation, stage, telemetry.SpanQuerySupplyChainSecurityAlerts, build)
	}
	routes := []failure.Route{
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
	runSupplyFailureRoutes(t, []failure.Route{{
		Name:   "persist suppression",
		Method: http.MethodPost,
		Path:   "/api/v0/supply-chain/impact/suppressions",
		Body: `{"suppression_id":"suppression-1","justification":"accepted_risk",` +
			`"authored_at":"2026-07-27T12:00:00Z","expires_at":"2026-08-01T12:00:00Z",` +
			`"reason":"compensating control verified","scope":{"cve_id":"CVE-2026-00001"}}`,
		Message:         "persist vulnerability suppression",
		PlainDetail:     true,
		NoFence:         true,
		HandlerSpanName: telemetry.SpanQueryVulnerabilitySuppressionMutation,
		Scope: func(ctx context.Context) context.Context {
			return auth.ContextWithAuthContext(ctx, auth.AuthContext{
				Mode: auth.AuthModeShared, SubjectClass: "shared_token", SubjectIDHash: "sha256:operator", AllScopes: true,
			})
		},
		Handler: func(err error) failure.Mounter {
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
