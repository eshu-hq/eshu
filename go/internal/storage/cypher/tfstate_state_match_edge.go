// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	terraformstate "github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/terraform"
)

type (
	TerraformStateOwnershipResolver   = terraformstate.TerraformStateOwnershipResolver
	TerraformStateConfigMatchQuery    = terraformstate.TerraformStateConfigMatchQuery
	TerraformStateConfigMatchResolver = terraformstate.TerraformStateConfigMatchResolver
)

func (w *CanonicalNodeWriter) resolveTerraformStateOwnership(ctx context.Context, rows []canonical.TerraformStateResourceRow) []canonical.TerraformStateResourceRow {
	return terraformstate.ResolveTerraformStateOwnership(ctx, rows, w.tfStateOwnershipResolver)
}

func (w *CanonicalNodeWriter) resolveTerraformStateConfigMatchAmbiguity(ctx context.Context, rows []canonical.TerraformStateResourceRow) []canonical.TerraformStateResourceRow {
	return terraformstate.ResolveTerraformStateConfigMatchAmbiguity(ctx, rows, w.tfStateConfigMatchResolver)
}

func (w *CanonicalNodeWriter) terraformStateMatchesConfigEdgeStatements(mat canonical.CanonicalMaterialization) []Statement {
	return wrapTerraformParts(terraformstate.TerraformStateMatchesConfigEdgeParts(mat, w.batchSize))
}
