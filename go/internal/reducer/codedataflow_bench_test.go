// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// benchmarkCodeTaintEvidenceCorpus builds a synthetic corpus of count
// code_taint_evidence facts, a realistic per-finding shape for
// BenchmarkDecodeCodeTaintEvidenceInput.
func benchmarkCodeTaintEvidenceCorpus(count int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, count)
	for i := 0; i < count; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("taint-%d", i),
			FactKind: facts.CodeTaintEvidenceFactKind,
			Payload: map[string]any{
				"function_uid":  fmt.Sprintf("uid:fn-%d", i),
				"repo_id":       fmt.Sprintf("repo-%d", i%50),
				"relative_path": fmt.Sprintf("src/pkg%d/handler.go", i%50),
				"function_name": "HandleRequest",
				"language":      "go",
				"kind":          "sql_injection",
				"sink_kind":     "sql_exec",
				"source_kind":   "http_request",
				"binding":       "req.Body",
				"source_line":   float64(10 + i%40),
				"sink_line":     float64(50 + i%40),
				"confidence":    0.85,
				"class_context": "Handler",
			},
		})
	}
	return envelopes
}

// BenchmarkDecodeCodeTaintEvidenceInput measures the typed-decode path
// (Contract System v1 Wave 4f S2, issue #4754) for the per-fact conversion
// LoadCodeTaintEvidence calls for every code_taint_evidence fact in a scope
// generation. This is the no-regression baseline against the pre-migration
// ad hoc payloadString/int/float coercion the postgres loader used to
// perform inline.
func BenchmarkDecodeCodeTaintEvidenceInput(b *testing.B) {
	envelopes := benchmarkCodeTaintEvidenceCorpus(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, env := range envelopes {
			_, _ = decodeCodeTaintEvidenceInput(env)
		}
	}
}

// benchmarkCodeInterprocEvidenceCorpus builds a synthetic corpus of count
// code_interproc_evidence facts for BenchmarkDecodeCodeInterprocEvidenceInput.
func benchmarkCodeInterprocEvidenceCorpus(count int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, count)
	for i := 0; i < count; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("interproc-%d", i),
			FactKind: facts.CodeInterprocEvidenceFactKind,
			Payload: map[string]any{
				"source_function_uid":  fmt.Sprintf("uid:source-%d", i),
				"sink_function_uid":    fmt.Sprintf("uid:sink-%d", i),
				"repo_id":              fmt.Sprintf("repo-%d", i%50),
				"relative_path":        fmt.Sprintf("src/pkg%d/handler.go", i%50),
				"source_function_name": "readRequest",
				"sink_function_name":   "execQuery",
				"language":             "go",
				"sink_kind":            "sql_exec",
				"source_kind":          "http_request",
				"confidence":           0.8,
				"why_trail": []map[string]any{
					{"role": "source", "function_id": "readRequest", "slot_kind": "param"},
					{"role": "sink", "function_id": "execQuery", "slot_kind": "arg"},
				},
			},
		})
	}
	return envelopes
}

// BenchmarkDecodeCodeInterprocEvidenceInput measures the typed-decode path
// for the per-fact conversion LoadCodeInterprocEvidence calls for every
// code_interproc_evidence fact in a scope generation.
func BenchmarkDecodeCodeInterprocEvidenceInput(b *testing.B) {
	envelopes := benchmarkCodeInterprocEvidenceCorpus(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, env := range envelopes {
			_, _ = decodeCodeInterprocEvidenceInput(env)
		}
	}
}

// BenchmarkExtractCodeTaintEvidenceRows measures the full row-extraction path
// (decode + row-build) end to end, mirroring what
// CodeTaintEvidenceMaterializationHandler.Handle does per generation.
func BenchmarkExtractCodeTaintEvidenceRows(b *testing.B) {
	envelopes := benchmarkCodeTaintEvidenceCorpus(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = ExtractCodeTaintEvidenceRowsWithQuarantine(envelopes)
	}
}

// BenchmarkExtractCodeInterprocEvidenceRows measures the full row-extraction
// path for the interproc family, mirroring
// CodeInterprocEvidenceMaterializationHandler.Handle.
func BenchmarkExtractCodeInterprocEvidenceRows(b *testing.B) {
	envelopes := benchmarkCodeInterprocEvidenceCorpus(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = ExtractCodeInterprocEvidenceRowsWithQuarantine(envelopes)
	}
}

// benchmarkShellExecCorpus builds a synthetic corpus of repository + file
// facts carrying embedded shell commands, for BenchmarkExtractShellExecRows.
func benchmarkShellExecCorpus(fileCount int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, fileCount+1)
	envelopes = append(envelopes, facts.Envelope{
		FactID:   "repo-bench",
		FactKind: factKindRepository,
		Payload:  map[string]any{"repo_id": "repo-bench"},
	})
	for i := 0; i < fileCount; i++ {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("file-%d", i),
			FactKind: factKindFile,
			Payload: map[string]any{
				"repo_id":       "repo-bench",
				"relative_path": fmt.Sprintf("cmd/tool%d/main.go", i),
				"parsed_file_data": map[string]any{
					"path": fmt.Sprintf("/repo/cmd/tool%d/main.go", i),
					"functions": []any{
						map[string]any{"name": "runTool", "line_number": 7, "uid": fmt.Sprintf("function:runTool-%d", i)},
					},
					"embedded_shell_commands": []any{
						map[string]any{
							"function_name":        "runTool",
							"function_line_number": 7,
							"line_number":          8,
							"api":                  "os/exec.CommandContext",
							"language":             "go",
						},
					},
				},
			},
		})
	}
	return envelopes
}

// BenchmarkExtractShellExecRows measures ExtractShellExecRows's typed-decode
// path (Contract System v1 Wave 4f S2, issue #4754) for a realistic
// repository+file corpus, the no-regression baseline against the
// pre-migration semanticPayloadString/payloadMap reads.
func BenchmarkExtractShellExecRows(b *testing.B) {
	envelopes := benchmarkShellExecCorpus(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExtractShellExecRows(envelopes)
	}
}
