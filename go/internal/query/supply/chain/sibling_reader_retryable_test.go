// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestSiblingStoreReadsAnswerRetryable503 is the #7549 retryability proof,
// following #7568: every sibling store read goes through the guarded
// PostgreSQL reader port in production (each store is built
// WithReadStore), so a stale reader or a pool-wait timeout must answer the
// retryable 503 backend_unavailable with Retry-After instead of a 500
// carrying the Go error text. A non-timeout reader failure stays a 500 with
// the stage_failed line, and a mapped 503 stays silent.
func TestSiblingStoreReadsAnswerRetryable503(t *testing.T) {
	ensureSiblingAdvisoryCapabilities()
	t.Parallel()

	branches := []struct {
		name       string
		target     string
		stage      string
		capability string
		build      func(err error) *Handler
	}{
		{
			name:       "explain read",
			target:     "/api/v0/supply-chain/impact/explain?finding_id=finding-1",
			stage:      "impact_explanation_query",
			capability: ImpactExplanationCapability,
			build: func(err error) *Handler {
				return &Handler{ImpactExplanations: errExplanationStore{err: err}}
			},
		},
		{
			name:       "explain cloud runtime probe read",
			target:     "/api/v0/supply-chain/impact/explain?finding_id=finding-1",
			stage:      "cloud_runtime_evidence",
			capability: ImpactExplanationCapability,
			build: func(err error) *Handler {
				digest := failedStageRow().SubjectDigest
				cloudGraph := &graph.FakeCloudRuntimeGraph{RowsByDigest: map[string][]map[string]any{
					digest: {cloudResourceGraphRow("uid-x", digest, "arn-x")},
				}}
				return &Handler{
					Neo4j:                  cloudGraph,
					ImpactExplanations:     errExplanationStore{row: siblingProbeExplanationRow()},
					CloudResourceInventory: &stubCloudInventory{err: err, rowsByDigest: cloudGraph.RowsByDigest},
				}
			},
		},
		{
			name:       "explain kubernetes runtime probe read",
			target:     "/api/v0/supply-chain/impact/explain?finding_id=finding-1",
			stage:      "kubernetes_runtime_evidence",
			capability: ImpactExplanationCapability,
			build: func(err error) *Handler {
				digest := failedStageRow().SubjectDigest
				return &Handler{
					Neo4j: &graph.FakeKubernetesRuntimeGraph{Rows: []map[string]any{{
						"matched_digest": digest, "workload_uid": "kw-1", "edge_scope_id": "scope-1", "edge_generation_id": "gen-1",
					}}},
					ImpactExplanations:          errExplanationStore{row: siblingProbeExplanationRow()},
					KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{err: err},
				}
			},
		},
		{
			name:       "explain runtime context probe read",
			target:     "/api/v0/supply-chain/impact/explain?finding_id=finding-1",
			stage:      "runtime_context",
			capability: ImpactExplanationCapability,
			build: func(err error) *Handler {
				return &Handler{
					ImpactExplanations: errExplanationStore{row: siblingProbeExplanationRow()},
					ImpactFindings: &graph.FakeRuntimeContextFindingStore{
						Rows: []impact.FindingRow{failedStageRow()},
						Err:  err,
					},
				}
			},
		},
		{
			name:       "impact packet read",
			target:     "/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1",
			stage:      "impact_explanation_query",
			capability: ImpactExplanationCapability,
			build: func(err error) *Handler {
				return &Handler{ImpactExplanations: errExplanationStore{err: err}, PacketResponder: stubImpactPacketResponder{}}
			},
		},
		{
			name:       "impact aggregate count",
			target:     "/api/v0/supply-chain/impact/findings/count",
			stage:      "impact_aggregate_count",
			capability: ImpactAggregateCapability,
			build: func(err error) *Handler {
				return &Handler{ImpactAggregates: errImpactAggregateStore{countErr: err}}
			},
		},
		{
			name:       "impact aggregate inventory",
			target:     "/api/v0/supply-chain/impact/inventory?group_by=impact_status&limit=10",
			stage:      "impact_aggregate_inventory",
			capability: ImpactAggregateCapability,
			build: func(err error) *Handler {
				return &Handler{ImpactAggregates: errImpactAggregateStore{inventoryErr: err}}
			},
		},
		{
			name:       "container image aggregate count",
			target:     "/api/v0/supply-chain/container-images/identities/count",
			stage:      "container_image_aggregate_count",
			capability: ContainerImageIdentityAggregateCapability,
			build: func(err error) *Handler {
				return &Handler{ContainerImageAggregates: errContainerImageAggregateStore{countErr: err}}
			},
		},
		{
			name:       "container image aggregate inventory",
			target:     "/api/v0/supply-chain/container-images/identities/inventory?group_by=outcome&limit=10",
			stage:      "container_image_aggregate_inventory",
			capability: ContainerImageIdentityAggregateCapability,
			build: func(err error) *Handler {
				return &Handler{ContainerImageAggregates: errContainerImageAggregateStore{inventoryErr: err}}
			},
		},
		{
			name:       "sbom attachment aggregate count",
			target:     "/api/v0/supply-chain/sbom-attestations/attachments/count",
			stage:      "sbom_attachment_aggregate_count",
			capability: SBOMAttestationAttachmentAggregateCapability,
			build: func(err error) *Handler {
				return &Handler{SBOMAttachmentAggregates: errSBOMAttachmentAggregateStore{countErr: err}}
			},
		},
		{
			name:       "sbom attachment aggregate inventory",
			target:     "/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=attachment_status&limit=10",
			stage:      "sbom_attachment_aggregate_inventory",
			capability: SBOMAttestationAttachmentAggregateCapability,
			build: func(err error) *Handler {
				return &Handler{SBOMAttachmentAggregates: errSBOMAttachmentAggregateStore{inventoryErr: err}}
			},
		},
		{
			name:       "security alert aggregate count",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/count?cve_id=CVE-2026-0001",
			stage:      "security_alert_aggregate_count",
			capability: SecurityAlertReconciliationAggregateCapability,
			build: func(err error) *Handler {
				return &Handler{SecurityAlertAggregates: errSecurityAlertAggregateStore{countErr: err}}
			},
		},
		{
			name:       "security alert aggregate inventory",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=reconciliation_status&limit=10",
			stage:      "security_alert_aggregate_inventory",
			capability: SecurityAlertReconciliationAggregateCapability,
			build: func(err error) *Handler {
				return &Handler{SecurityAlertAggregates: errSecurityAlertAggregateStore{inventoryErr: err}}
			},
		},
		{
			name:       "advisory catalog list",
			target:     "/api/v0/supply-chain/advisories?limit=10",
			stage:      "advisory_catalog_query",
			capability: advisory.CatalogCapability,
			build: func(err error) *Handler {
				return &Handler{AdvisoryCatalog: errAdvisoryCatalogStore{err: err}}
			},
		},
		{
			name:       "advisory evidence list",
			target:     "/api/v0/supply-chain/advisories/evidence?limit=10&cve_id=CVE-2026-0001",
			stage:      "advisory_evidence_query",
			capability: advisory.EvidenceCapability,
			build: func(err error) *Handler {
				return &Handler{AdvisoryEvidence: errAdvisoryEvidenceStore{err: err}}
			},
		},
		{
			name:       "vulnerability detail",
			target:     "/api/v0/supply-chain/vulnerabilities/CVE-2026-0001",
			stage:      "vulnerability_detail_query",
			capability: advisory.EvidenceCapability,
			build: func(err error) *Handler {
				return &Handler{AdvisoryEvidence: errAdvisoryEvidenceStore{err: err}}
			},
		},
		{
			name:       "container image identities list",
			target:     "/api/v0/supply-chain/container-images/identities?limit=10&digest=sha256:abc",
			stage:      "container_image_identity_query",
			capability: ContainerImageIdentitiesCapability,
			build: func(err error) *Handler {
				return &Handler{ContainerImageIdentities: errContainerImageStore{err: err}}
			},
		},
		{
			name:       "sbom attachments list",
			target:     "/api/v0/supply-chain/sbom-attestations/attachments?limit=10&subject_digest=sha256:abc",
			stage:      "sbom_attachment_query",
			capability: SBOMAttestationAttachmentsCapability,
			build: func(err error) *Handler {
				return &Handler{SBOMAttachments: errSBOMAttachmentStore{err: err}}
			},
		},
		{
			name:       "security alert reconciliations list",
			target:     "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001",
			stage:      "security_alert_reconciliation_query",
			capability: SecurityAlertReconciliationsCapability,
			build: func(err error) *Handler {
				return &Handler{SecurityAlerts: errSecurityAlertStore{err: err}}
			},
		},
	}
	verdicts := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"reader stale", fmt.Errorf("read store: %w", db.ErrReaderStale), http.StatusServiceUnavailable},
		{
			"reader pool wait timeout",
			fmt.Errorf("read store: %w", errors.Join(db.ErrReaderUnavailable, context.DeadlineExceeded)),
			http.StatusServiceUnavailable,
		},
		{"bare reader unavailable", fmt.Errorf("read store: %w", db.ErrReaderUnavailable), http.StatusInternalServerError},
	}

	for _, branch := range branches {
		for _, verdict := range verdicts {
			t.Run(branch.name+"/"+verdict.name, func(t *testing.T) {
				t.Parallel()

				var logBuf bytes.Buffer
				handler := branch.build(verdict.err)
				handler.Logger = slog.New(slog.NewJSONHandler(&logBuf, nil))

				rec := serveSiblingRoute(t, handler, branch.target)

				if rec.Code != verdict.wantStatus {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, verdict.wantStatus, rec.Body.String())
				}
				failed := recordsWithEvent(decodeLogRecords(t, &logBuf), "supply_chain_query.stage_failed")
				if verdict.wantStatus == http.StatusInternalServerError {
					if rec.Header().Get("Retry-After") != "" {
						t.Fatalf("Retry-After = %q on a non-retryable 500, want none", rec.Header().Get("Retry-After"))
					}
					if len(failed) != 1 || failed[0]["stage"] != branch.stage {
						t.Fatalf("stage_failed records = %#v, want exactly one for stage %q", failed, branch.stage)
					}
					return
				}
				if len(failed) != 0 {
					t.Fatalf("stage_failed records = %d on a mapped 503, want 0; log=%s", len(failed), logBuf.String())
				}
				if seconds, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || seconds < 1 {
					t.Fatalf("Retry-After = %q, want a positive integer", rec.Header().Get("Retry-After"))
				}
				var body struct {
					Error *querycontract.ErrorEnvelope `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == nil {
					t.Fatalf("body = %s, want an error envelope: %v", rec.Body.String(), err)
				}
				if body.Error.Code != querycontract.ErrorCodeBackendUnavailable {
					t.Fatalf("error code = %q, want %q", body.Error.Code, querycontract.ErrorCodeBackendUnavailable)
				}
				if body.Error.Capability != branch.capability {
					t.Fatalf("error capability = %q, want %q", body.Error.Capability, branch.capability)
				}
			})
		}
	}
}
