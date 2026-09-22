// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

// This file is the golang root's compatibility surface for the prescan
// family (issue #6774): the pre-parse Go package/file evidence collection
// moved to internal/parser/golang/prescan, and this stanza keeps every
// symbol internal/parser (outside this module tree's edit scope for #6774)
// still calls compiling unchanged. Each entry is a thin forwarder or type
// alias with no behavior change; delete an entry once its last external
// caller has moved to call the prescan package directly.
//
// A family move adds a stanza here rather than creating a second
// compat_*.go file, matching the reducer root's compat_*.go convention
// (docs/internal/design/reducer-target-tree.md).
//
// Stanzas merged here:
//   - prescan family (PreScan, PreScanFileEvidence,
//     ImportedDirectMethodCallRootsWithInterfaceReturns; callers in
//     internal/parser/go_language.go and
//     internal/parser/go_package_interface_prescan.go)

import (
	"github.com/eshu-hq/eshu/go/internal/parser/golang/prescan"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// PrescanFileEvidence carries every per-file Go evidence type the parent
// package's PreScanGoPackageSemanticRoots loop needs to aggregate. See
// prescan.PrescanFileEvidence for the full contract.
type PrescanFileEvidence = prescan.PrescanFileEvidence

// PreScan returns deterministic Go symbols used by collector import-map
// prescans. See prescan.PreScan for the full contract.
func PreScan(parser *tree_sitter.Parser, path string) ([]string, error) {
	return prescan.PreScan(parser, path)
}

// PreScanFileEvidence reads, parses, and walks a single Go source file once
// to populate every evidence type the parent package prescan needs. See
// prescan.PreScanFileEvidence for the full contract.
func PreScanFileEvidence(parser *tree_sitter.Parser, path string) (*PrescanFileEvidence, error) {
	return prescan.PreScanFileEvidence(parser, path)
}

// ImportedDirectMethodCallRootsWithInterfaceReturns returns qualified method
// roots for one Go file using package-level local-interface return metadata.
// See prescan.ImportedDirectMethodCallRootsWithInterfaceReturns for the full
// contract.
func ImportedDirectMethodCallRootsWithInterfaceReturns(
	parser *tree_sitter.Parser,
	path string,
	interfaceMethodReturns map[string]string,
) (GoDirectMethodCallRoots, error) {
	return prescan.ImportedDirectMethodCallRootsWithInterfaceReturns(parser, path, interfaceMethodReturns)
}
