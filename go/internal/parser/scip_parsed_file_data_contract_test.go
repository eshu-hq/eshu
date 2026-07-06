// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
	scippb "github.com/scip-code/scip/bindings/go/scip"
)

// TestSCIPFunctionCallsRoundTripThroughTypedContract proves the factschema
// codegraphv1.SCIPFunctionCall struct + DecodeParsedFileDataSCIPFunctionCalls
// accessor faithfully model the SCIP importer's real function_calls_scip edge
// output: decoding the producer's own payload through the typed accessor
// recovers every edge field the reducer's SCIP code-call extractor reads
// (caller/callee symbol, file, line, callee_name, ref_line). It binds the typed
// contract to the single SCIP producer as the authoritative closed edge shape
// (issue #4750 S1) without changing the emitted bytes.
func TestSCIPFunctionCallsRoundTripThroughTypedContract(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	callerPath := filepath.Join(repoRoot, "caller.py")
	calleePath := filepath.Join(repoRoot, "callee.py")
	writeSCIPTestFile(t, callerPath, "def handle():\n    return callee(1)\n")
	writeSCIPTestFile(t, calleePath, "def callee(x):\n    return x\n")

	callerSymbol := "pkg/caller#handle()."
	calleeSymbol := "pkg/callee#callee()."
	indexPath := filepath.Join(repoRoot, "index.scip")
	writeSCIPIndexFixture(
		t,
		indexPath,
		&scippb.Index{
			Documents: []*scippb.Document{
				{
					RelativePath: "callee.py",
					Language:     "python",
					Occurrences: []*scippb.Occurrence{
						{
							Range:       []int32{0, 0, 0, 6},
							Symbol:      calleeSymbol,
							SymbolRoles: int32(scippb.SymbolRole_Definition),
						},
					},
					Symbols: []*scippb.SymbolInformation{
						{
							Symbol:      calleeSymbol,
							DisplayName: "callee(x: int) -> int",
							Kind:        scippb.SymbolInformation_Function,
						},
					},
				},
				{
					RelativePath: "caller.py",
					Language:     "python",
					Occurrences: []*scippb.Occurrence{
						{
							Range:       []int32{0, 0, 0, 6},
							Symbol:      callerSymbol,
							SymbolRoles: int32(scippb.SymbolRole_Definition),
						},
						{
							Range:  []int32{1, 11, 1, 17},
							Symbol: calleeSymbol,
						},
					},
					Symbols: []*scippb.SymbolInformation{
						{
							Symbol:      callerSymbol,
							DisplayName: "handle() -> int",
							Kind:        scippb.SymbolInformation_Function,
						},
					},
				},
			},
		},
	)

	got, err := (SCIPIndexParser{}).Parse(indexPath, repoRoot)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	callerFile := got.Files[callerPath]
	rawEdges, _ := callerFile["function_calls_scip"].([]map[string]any)
	if len(rawEdges) != 1 {
		t.Fatalf("len(raw function_calls_scip) = %d, want 1", len(rawEdges))
	}

	edges, err := factschema.DecodeParsedFileDataSCIPFunctionCalls(callerFile)
	if err != nil {
		t.Fatalf("DecodeParsedFileDataSCIPFunctionCalls() error = %v", err)
	}
	if len(edges) != len(rawEdges) {
		t.Fatalf("typed edge count = %d, raw = %d", len(edges), len(rawEdges))
	}

	raw := rawEdges[0]
	edge := edges[0]
	if edge.CallerSymbol != raw["caller_symbol"] || edge.CalleeSymbol != raw["callee_symbol"] {
		t.Fatalf("symbols = %q/%q, raw = %v/%v", edge.CallerSymbol, edge.CalleeSymbol, raw["caller_symbol"], raw["callee_symbol"])
	}
	if edge.CallerFile != raw["caller_file"] || edge.CalleeFile != raw["callee_file"] {
		t.Fatalf("files = %q/%q, raw = %v/%v", edge.CallerFile, edge.CalleeFile, raw["caller_file"], raw["callee_file"])
	}
	if edge.CalleeName != raw["callee_name"] {
		t.Fatalf("CalleeName = %q, raw = %v", edge.CalleeName, raw["callee_name"])
	}
	// The producer emits Go ints for the line fields; the typed int fields must
	// recover them exactly.
	if edge.CallerLine != raw["caller_line"] || edge.CalleeLine != raw["callee_line"] || edge.RefLine != raw["ref_line"] {
		t.Fatalf("lines = %d/%d/%d, raw = %v/%v/%v", edge.CallerLine, edge.CalleeLine, edge.RefLine, raw["caller_line"], raw["callee_line"], raw["ref_line"])
	}
}
