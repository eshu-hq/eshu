// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// Replatforming API/MCP parity + refusal-safety proof fixtures (issue #1968).
//
// This file owns the deterministic, fixture-backed IaC management store and the
// shared request scope used by the replatforming proof gate. The proof drives
// the three replatforming serving routes
//
//   - POST /api/v0/replatforming/plans             (compose_replatforming_plan)
//   - POST /api/v0/replatforming/ownership-packets (find_unmanaged_resource_owners)
//   - POST /api/v0/replatforming/rollups           (get_replatforming_rollups)
//
// through the HTTP surface and the MCP dispatch path against ONE handler
// instance, then asserts both surfaces return the identical canonical envelope.
//
// The fixture set is intentionally a refusal-safety corpus: it contains a
// safety-approved ready import candidate, a security_review_required finding, an
// ambiguous (contested ownership) finding, a stale finding, an unknown finding,
// and a terraform_state_only (state-only, not importable) finding, plus a
// finding whose raw tags carry a credential-shaped key/value. Each surface must
// keep refused, ambiguous, stale, unknown, and state-only items distinct from
// ready ones, never fold a refused item into a clean or ready bucket, and never
// leak a raw secret value.

import (
	"context"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// fixtureManagementStore is a deterministic in-memory IaCManagementStore that
// returns the same bounded rows to every replatforming surface. It paginates
// like the real store so truncation and offset parity can be proved.
type fixtureManagementStore struct {
	rows []query.IaCManagementFindingRow
}

// ListUnmanagedCloudResources returns the bounded fixture page for the filter,
// applying offset then limit exactly as the Postgres-backed store does.
func (s fixtureManagementStore) ListUnmanagedCloudResources(
	_ context.Context,
	filter query.IaCManagementFilter,
) ([]query.IaCManagementFindingRow, error) {
	rows := append([]query.IaCManagementFindingRow(nil), s.rows...)
	if filter.Offset >= len(rows) {
		return nil, nil
	}
	rows = rows[filter.Offset:]
	if filter.Limit > 0 && len(rows) > filter.Limit {
		rows = rows[:filter.Limit]
	}
	return rows, nil
}

// CountUnmanagedCloudResources returns the total fixture row count, independent
// of paging, so truncation math agrees across surfaces.
func (s fixtureManagementStore) CountUnmanagedCloudResources(
	_ context.Context,
	_ query.IaCManagementFilter,
) (int, error) {
	return len(s.rows), nil
}

// replatformingSecretTagValue is a credential-shaped raw tag value. It must
// never appear in any replatforming response payload; the leakage assertion
// scans the full serialized envelope for it.
const replatformingSecretTagValue = "AKIA_TOTALLY_SECRET_VALUE_DO_NOT_LEAK"

// replatformingProofRows returns the refusal-safety corpus shared by every
// surface.
func replatformingProofRows() []query.IaCManagementFindingRow {
	return []query.IaCManagementFindingRow{
		// Ready: a safety-approved cloud_only Lambda with a supported import
		// mapping. This is the ONLY row that may surface a ready import candidate.
		{
			ID:                    "fact:ready-lambda",
			Provider:              "aws",
			AccountID:             "123456789012",
			Region:                "us-east-1",
			ResourceType:          "lambda",
			ResourceID:            "function:payments-api",
			ARN:                   "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
			FindingKind:           "orphaned_cloud_resource",
			ManagementStatus:      "cloud_only",
			Confidence:            0.95,
			ScopeID:               "aws:123456789012:us-east-1:lambda",
			GenerationID:          "generation:aws-1",
			SourceSystem:          "aws",
			ServiceCandidates:     []string{"payments-api"},
			EnvironmentCandidates: []string{"prod"},
		},
		// Security review required: a cloud_only resource flagged
		// security-sensitive with a credential-shaped raw tag. The safety gate
		// must refuse the import; the effective source state must be rejected.
		{
			ID:                    "fact:secret-store",
			Provider:              "aws",
			AccountID:             "123456789012",
			Region:                "us-east-1",
			ResourceType:          "secretsmanager",
			ResourceID:            "secret:prod/payments/db",
			ARN:                   "arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/payments/db",
			FindingKind:           "unmanaged_cloud_resource",
			ManagementStatus:      "cloud_only",
			Confidence:            0.9,
			ScopeID:               "aws:123456789012:us-east-1:secretsmanager",
			GenerationID:          "generation:aws-1",
			SourceSystem:          "aws",
			WarningFlags:          []string{"security_sensitive_resource"},
			ServiceCandidates:     []string{"payments-api"},
			EnvironmentCandidates: []string{"prod"},
			Tags: map[string]string{
				"secret_access_key": replatformingSecretTagValue,
			},
			Evidence: []query.IaCManagementEvidenceRow{{
				ID:             "evidence:secret",
				SourceSystem:   "aws",
				EvidenceType:   "aws_secret_value",
				ScopeID:        "aws:123456789012:us-east-1:secretsmanager",
				Key:            "tag:secret_access_key",
				Value:          replatformingSecretTagValue,
				Confidence:     0.9,
				ProvenanceOnly: true,
			}},
		},
		// Ambiguous: two conflicting service and environment candidates.
		// Ownership must stay contested; the rollup must count this as ambiguous.
		{
			ID:                    "fact:ambiguous-queue",
			Provider:              "aws",
			AccountID:             "123456789012",
			Region:                "us-east-1",
			ResourceType:          "sqs",
			ResourceID:            "payments-events",
			ARN:                   "arn:aws:sqs:us-east-1:123456789012:payments-events",
			FindingKind:           "ambiguous_cloud_resource",
			ManagementStatus:      "ambiguous_management",
			Confidence:            0.5,
			ScopeID:               "aws:123456789012:us-east-1:sqs",
			GenerationID:          "generation:aws-1",
			SourceSystem:          "aws",
			ServiceCandidates:     []string{"payments-api", "billing-api"},
			EnvironmentCandidates: []string{"prod", "staging"},
		},
		// Stale: a stale IaC candidate. Must be refused and reported not-fresh.
		{
			ID:                    "fact:stale-bucket",
			Provider:              "aws",
			AccountID:             "123456789012",
			Region:                "us-east-1",
			ResourceType:          "s3",
			ResourceID:            "payments-prod-logs",
			ARN:                   "arn:aws:s3:::payments-prod-logs",
			FindingKind:           "unmanaged_cloud_resource",
			ManagementStatus:      "stale_iac_candidate",
			Confidence:            0.6,
			ScopeID:               "aws:123456789012:us-east-1:s3",
			GenerationID:          "generation:aws-1",
			SourceSystem:          "aws",
			ServiceCandidates:     []string{"payments-api"},
			EnvironmentCandidates: []string{"prod"},
		},
		// Unknown: a coverage/permission gap. Must be the fail-safe unknown state
		// and refused, never a confident answer.
		{
			ID:               "fact:unknown-resource",
			Provider:         "aws",
			AccountID:        "123456789012",
			Region:           "us-east-1",
			ResourceType:     "ec2",
			ResourceID:       "i-0deadbeef",
			ARN:              "arn:aws:ec2:us-east-1:123456789012:instance/i-0deadbeef",
			FindingKind:      "unknown_cloud_resource",
			ManagementStatus: "unknown_management",
			Confidence:       0.2,
			ScopeID:          "aws:123456789012:us-east-1:ec2",
			GenerationID:     "generation:aws-1",
			SourceSystem:     "aws",
		},
		// State-only: terraform_state_only is derived but NOT importable. It is a
		// non-rejected, non-ready item that must land in needs_review.
		{
			ID:                    "fact:state-only-table",
			Provider:              "aws",
			AccountID:             "123456789012",
			Region:                "us-east-1",
			ResourceType:          "dynamodb",
			ResourceID:            "GameScores",
			ARN:                   "arn:aws:dynamodb:us-east-1:123456789012:table/GameScores",
			FindingKind:           "unmanaged_cloud_resource",
			ManagementStatus:      "terraform_state_only",
			Confidence:            0.8,
			ScopeID:               "aws:123456789012:us-east-1:dynamodb",
			GenerationID:          "generation:aws-1",
			SourceSystem:          "aws",
			ServiceCandidates:     []string{"leaderboard"},
			EnvironmentCandidates: []string{"prod"},
		},
	}
}

// mountReplatformingHandler mounts one IaCHandler backed by the shared fixture
// store so the HTTP and MCP surfaces are driven against the exact same handler.
func mountReplatformingHandler(t *testing.T, profile query.QueryProfile) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	handler := &query.IaCHandler{
		Profile:    profile,
		Management: fixtureManagementStore{rows: replatformingProofRows()},
	}
	handler.Mount(mux)
	return mux
}

// replatformingArgs is the shared MCP scope both surfaces request. account_id
// and finding_kinds bound the read; the MCP tool body builders default limit to
// 100 and offset to 0.
func replatformingArgs() map[string]any {
	return map[string]any{
		"account_id": "123456789012",
		"region":     "us-east-1",
		"finding_kinds": []any{
			"ambiguous_cloud_resource",
			"orphaned_cloud_resource",
			"unmanaged_cloud_resource",
			"unknown_cloud_resource",
		},
	}
}

// replatformingHTTPBody mirrors replatformingArgs after the MCP body builders
// apply their defaults (limit 100, offset 0), so the HTTP request body is
// byte-identical to what the MCP dispatch forwards. The plan surface also
// requires scope_kind, supplied through extra.
func replatformingHTTPBody(extra map[string]any) map[string]any {
	body := map[string]any{
		"account_id": "123456789012",
		"region":     "us-east-1",
		"finding_kinds": []string{
			"ambiguous_cloud_resource",
			"orphaned_cloud_resource",
			"unmanaged_cloud_resource",
			"unknown_cloud_resource",
		},
		"limit":  100,
		"offset": 0,
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}
