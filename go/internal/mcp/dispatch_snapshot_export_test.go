// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// snapshotExportRefusingContent fails code-topic investigation the way the
// guarded reader does on a PostgreSQL build not qualified for shared-snapshot
// export (#7957).
type snapshotExportRefusingContent struct {
	content.FakePortContentStore
}

func (snapshotExportRefusingContent) InvestigateCodeTopic(context.Context, codequery.CodeTopicInvestigationRequest) ([]codequery.CodeTopicEvidenceRow, error) {
	return nil, fmt.Errorf("investigate: %w", db.ErrSnapshotExportUnqualified)
}

// TestDispatchInvestigateCodeTopicSurfacesSnapshotExportRefusal proves the MCP
// face of the #7957 refusal: an isError result whose envelope carries
// backend_unavailable, the fixed message, and details.reason, with no
// retry_after_seconds hint, because MCP forwards the envelope, not headers.
func TestDispatchInvestigateCodeTopicSurfacesSnapshotExportRefusal(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	(&query.CodeHandler{Content: snapshotExportRefusingContent{}, Profile: query.ProfileLocalAuthoritative}).Mount(mux)
	result, err := dispatchTool(context.Background(), mux, "investigate_code_topic", map[string]any{"topic": "auth"}, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want an isError envelope result", err)
	}
	if result == nil || !result.IsError || result.Envelope == nil || result.Envelope.Error == nil {
		t.Fatalf("result = %#v, want isError with an error envelope", result)
	}
	got := result.Envelope.Error
	if got.Code != query.ErrorCodeBackendUnavailable || got.Message != querycontract.ErrSnapshotExportRefused.Error() ||
		got.Details["reason"] != querycontract.SnapshotExportUnqualifiedReason {
		t.Fatalf("error envelope = %#v", got)
	}
	if _, hinted := got.Details["retry_after_seconds"]; hinted {
		t.Fatalf("permanent refusal carries a retry hint: %#v", got.Details)
	}
}
