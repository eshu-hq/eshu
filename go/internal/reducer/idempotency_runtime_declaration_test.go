// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/runtime/declaration"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1"
)

// replayRecordingRuntimeDeclarationWriter records every declaration row the
// handler writes so the replay case can compare contents across runs.
type replayRecordingRuntimeDeclarationWriter struct {
	rows []map[string]any
}

func (w *replayRecordingRuntimeDeclarationWriter) WriteRuntimeDeclarations(_ context.Context, rows []map[string]any, _, _ string) error {
	w.rows = append(w.rows, rows...)
	return nil
}

func (w *replayRecordingRuntimeDeclarationWriter) RetractRuntimeDeclarations(context.Context, []string, string) error {
	return nil
}

func (w *replayRecordingRuntimeDeclarationWriter) RetractRuntimeDeclarationsByPath(context.Context, map[string][]string, string) error {
	return nil
}

// runtimeDeclarationReplayCase proves replaying the same
// runtime_declaration.version facts writes byte-identical rows under the same
// five-part declaration key (#8816).
func runtimeDeclarationReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainRuntimeDeclarationProjection,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &replayRecordingRuntimeDeclarationWriter{}
			handler := declaration.Handler{
				FactLoader: &stubFactLoader{envelopes: fencedFacts(runtimeDeclarationReplayFacts(t))},
				Writer:     writer,
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
					return true, nil
				},
			}
			intent := replayIntent(DomainRuntimeDeclarationProjection, "scope-runtime", nil)
			if _, err := handler.Handle(drainContext(), intent); err != nil {
				t.Fatalf("runtime declaration Handle: %v", err)
			}
			out := make([]idempotencyRow, 0, len(writer.rows))
			for _, row := range writer.rows {
				out = append(out, idempotencyRow{
					identity: fmt.Sprintf("%v|%v|%v|%v|%v",
						row["repo_id"], row["relative_path"], row["line_number"], row["runtime"], row["order_index"]),
					contents: stableRowContents(row),
				})
			}
			return out
		},
	}
}

func runtimeDeclarationReplayFacts(t *testing.T) []facts.Envelope {
	t.Helper()
	major := 20
	out := []facts.Envelope{{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-rt"}}}
	for order, raw := range []string{"20.11.1", "lts/iron"} {
		version := runtimedeclarationv1.Version{
			RepoID: "repo-rt", RelativePath: ".tool-versions", ScopeDir: ".", LineNumber: 1, OrderIndex: order,
			SourceKind: "tool_versions", Runtime: "node", RawSpec: raw, SpecKind: runtimedeclarationv1.SpecKindAlias,
		}
		if order == 0 {
			version.SpecKind = runtimedeclarationv1.SpecKindExact
			version.VersionMajor = &major
		}
		payload, err := factschema.EncodeRuntimeDeclarationVersion(version)
		if err != nil {
			t.Fatalf("encode runtime declaration: %v", err)
		}
		out = append(out, facts.Envelope{
			FactKind:      facts.RuntimeDeclarationVersionFactKind,
			SchemaVersion: facts.RuntimeDeclarationSchemaVersionV1,
			Payload:       payload,
		})
	}
	return out
}
