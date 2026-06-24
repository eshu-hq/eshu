// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestWorkItemValidateRejectsPartialTenantBoundary(t *testing.T) {
	t.Parallel()

	item := WorkItem{
		WorkItemID:          "work-item-tenant-boundary",
		RunID:               "run-tenant-boundary",
		CollectorKind:       scope.CollectorAWS,
		CollectorInstanceID: "collector-aws",
		SourceSystem:        string(scope.CollectorAWS),
		ScopeID:             "aws:scope:lambda",
		TenantID:            "tenant-a",
		AcceptanceUnitID:    "aws:scope:lambda",
		SourceRunID:         "source-run-tenant-boundary",
		GenerationID:        "generation-tenant-boundary",
		Status:              WorkItemStatusPending,
		CreatedAt:           time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC),
	}

	if err := item.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want partial tenant boundary rejection")
	}
}
