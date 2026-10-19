// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/decode"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestWriteCanonicalProjectionEmptyWarnsOnlyOnFileFacts pins review F1 on
// #7671: an empty materialization warns only when file facts were dropped
// upstream (the tripwire). Fact shapes with no file rows are legitimate
// canonical-empty and must stay below warn (debug, silent at Info level).
func TestWriteCanonicalProjectionEmptyWarnsOnlyOnFileFacts(t *testing.T) {
	t.Parallel()

	fileFacts := []facts.Envelope{{
		FactID:       "fact-file-0",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		FactKind:     decode.FactKindFileObserved,
		ObservedAt:   time.Date(2026, time.October, 19, 4, 0, 0, 0, time.UTC),
		Payload:      map[string]any{"relative_path": "main.go"},
	}}
	nonFileFacts := []facts.Envelope{{
		FactID:       "fact-repo-0",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		FactKind:     "repository",
		ObservedAt:   time.Date(2026, time.October, 19, 4, 0, 0, 0, time.UTC),
		Payload:      map[string]any{"repo_id": "repo-123"},
	}}

	scopeValue := scope.IngestionScope{ScopeID: "scope-123"}

	var fileLogs bytes.Buffer
	fileRuntime := Runtime{Logger: slog.New(slog.NewJSONHandler(&fileLogs, nil))}
	if err := fileRuntime.writeCanonicalProjection(context.Background(), scopeValue, "generation-456", fileFacts, canonical.CanonicalMaterialization{}); err != nil {
		t.Fatalf("writeCanonicalProjection(file facts) error = %v, want nil", err)
	}
	if !strings.Contains(fileLogs.String(), `"level":"WARN"`) {
		t.Fatalf("file-fact empty materialization logs = %q, want a WARN tripwire", fileLogs.String())
	}

	var bareLogs bytes.Buffer
	bareRuntime := Runtime{Logger: slog.New(slog.NewJSONHandler(&bareLogs, nil))}
	if err := bareRuntime.writeCanonicalProjection(context.Background(), scopeValue, "generation-456", nonFileFacts, canonical.CanonicalMaterialization{}); err != nil {
		t.Fatalf("writeCanonicalProjection(non-file facts) error = %v, want nil", err)
	}
	if strings.Contains(bareLogs.String(), `"level":"WARN"`) {
		t.Fatalf("non-file empty materialization logs = %q, want no WARN (legitimate canonical-empty)", bareLogs.String())
	}
}
