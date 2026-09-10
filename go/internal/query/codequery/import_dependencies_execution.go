// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/imports"
)

// This file holds the thin *CodeHandler execution surface of the
// import-dependency family. Row reads, module scopes, and shaping live in
// the imports leaf (builders and response shaping already live in
// codemodel); the methods stay here because Go requires methods to live
// in their type's package. Four of them carry queryplan source_sha256
// pins (see go/internal/queryplan/testdata/query-source-coverage.yaml
// with their QP-CODE-IMPORT entry ids): edit their bodies only with a
// manifest update in the same change.

// importDependencyRows dispatches one import-dependency read through the
// imports leaf. The staying dispatcher and tests name this spelling.
func (h *CodeHandler) importDependencyRows(
	ctx context.Context,
	req codemodel.ImportDependencyRequest,
) ([]map[string]any, error) {
	return imports.Rows(ctx, h.Neo4j, req)
}

// uniqueImportDependencyScopes dedupes module file membership rows
// through the imports leaf. Tests name this spelling.
func uniqueImportDependencyScopes(rows []map[string]any, pathKey string) []map[string]any {
	return imports.UniqueScopes(rows, pathKey)
}
