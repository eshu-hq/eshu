// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"testing"

	codedataflowv1 "github.com/eshu-hq/eshu-sdk/factschema/codedataflow/v1"
	codegraphv1 "github.com/eshu-hq/eshu-sdk/factschema/codegraph/v1"
	documentationv1 "github.com/eshu-hq/eshu-sdk/factschema/documentation/v1"
	runtimedeclarationv1 "github.com/eshu-hq/eshu-sdk/factschema/runtimedeclaration/v1"
	scannerworkerv1 "github.com/eshu-hq/eshu-sdk/factschema/scannerworker/v1"
	submodulev1 "github.com/eshu-hq/eshu-sdk/factschema/submodule/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// codeDecodeCases covers the code-family seams (codegraph, codedataflow,
// documentation, submodule, scanner worker, runtime declaration).
func codeDecodeCases() []decodeCase {
	return []decodeCase{
		{
			name: "CodegraphFile", kind: "file", target: &codegraphv1.File{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCodegraphFile(env) },
			requiredField: "repo_id",
			spotFields:    map[string]any{"repo_id": "github.com/example/repo", "relative_path": "main.go"},
		},
		{
			name: "CodegraphRepository", kind: "repository", target: &codegraphv1.Repository{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCodegraphRepository(env) },
			requiredField: "repo_id",
			spotFields:    map[string]any{"repo_id": "github.com/example/repo"},
		},
		{
			name: "CodeDataflowScanned", kind: "code_dataflow_scanned", target: &codedataflowv1.DataflowScanned{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCodeDataflowScanned(env) },
			requiredField: "repo_id", typeMismatch: true,
			spotFields: map[string]any{"repo_id": "github.com/example/repo"},
		},
		{
			name: "CodeDataflowFunction", kind: "code_dataflow_function", target: &codedataflowv1.DataflowFunction{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCodeDataflowFunction(env) },
			requiredField: "function_name",
			spotFields:    map[string]any{"repo_id": "github.com/example/repo", "function_name": "handleRequest"},
		},
		{
			name: "CodeFunctionSummary", kind: "code_function_summary", target: &codedataflowv1.FunctionSummary{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCodeFunctionSummary(env) },
			requiredField: "function_id",
			spotFields:    map[string]any{"function_id": "repo:main.go:handleRequest"},
		},
		{
			name: "CodeFunctionSource", kind: "code_function_source", target: &codedataflowv1.FunctionSource{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCodeFunctionSource(env) },
			requiredField: "kind",
			spotFields:    map[string]any{"function_id": "repo:main.go:handleRequest", "kind": "definition"},
		},
		{
			name: "CodeTaintEvidence", kind: "code_taint_evidence", target: &codedataflowv1.TaintEvidence{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCodeTaintEvidence(env) },
			requiredField: "function_uid",
			spotFields:    map[string]any{"function_uid": "repo:pkg/file.go:Sink"},
		},
		{
			name: "CodeInterprocEvidence", kind: "code_interproc_evidence", target: &codedataflowv1.InterprocEvidence{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCodeInterprocEvidence(env) },
			requiredField: "sink_function_uid",
			spotFields:    map[string]any{"source_function_uid": "repo:a.go:Source", "sink_function_uid": "repo:b.go:Sink"},
		},
		{
			name: "SubmodulePin", kind: "submodule.pin", target: &submodulev1.Pin{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeSubmodulePin(env) },
			requiredField: "submodule_path",
			spotFields:    map[string]any{"parent_repo_id": "github.com/example/repo", "submodule_path": "third_party/lib"},
		},
		{
			name: "DocumentationDocument", kind: "documentation_document", target: &documentationv1.Document{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeDocumentationDocument(env) },
			requiredField: "document_id",
			spotFields:    map[string]any{"document_id": "doc-architecture-overview"},
		},
		{
			name: "DocumentationEntityMention", kind: "documentation_entity_mention", target: &documentationv1.EntityMention{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeDocumentationEntityMention(env) },
			requiredField: "section_id",
			spotFields:    map[string]any{"document_id": "doc-architecture-overview", "resolution_status": "resolved"},
		},
		{
			name: "ScannerWorkerAnalysis", kind: "scanner_worker.analysis", target: &scannerworkerv1.Analysis{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeScannerWorkerAnalysis(env) },
			requiredField: "analyzer",
			spotFields:    map[string]any{"analyzer": "govulncheck", "analysis_status": "complete"},
		},
		{
			name: "RuntimeDeclarationVersion", kind: "runtime_declaration.version", target: &runtimedeclarationv1.Version{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeRuntimeDeclarationVersion(env) },
			requiredField: "raw_spec",
			spotFields:    map[string]any{"repo_id": "github.com/example/repo", "runtime": "go", "raw_spec": "go 1.24"},
		},
	}
}

func TestCodeDecodeSeams(t *testing.T) {
	t.Parallel()
	runDecodeCases(t, codeDecodeCases())
}
