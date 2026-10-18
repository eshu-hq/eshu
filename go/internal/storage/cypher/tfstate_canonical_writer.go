// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	terraformstate "github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/terraform"
)

const canonicalPhaseTerraformState = "terraform_state"

const (
	canonicalTerraformStateResourceUpsertCypher = terraformstate.TerraformStateResourceUpsertCypher
	canonicalTerraformStateModuleUpsertCypher   = terraformstate.TerraformStateModuleUpsertCypher
	canonicalTerraformStateOutputUpsertCypher   = terraformstate.TerraformStateOutputUpsertCypher
)

var terraformStateResourceAttributeRemoveCypherByType = terraformstate.ResourceAttributeRemoveCypherByType

func (w *CanonicalNodeWriter) buildTerraformStateStatements(mat canonical.CanonicalMaterialization) []Statement {
	return wrapTerraformParts(terraformstate.BuildTerraformStateStatements(mat, w.searchKey, w.batchSize))
}

func terraformStateResourceRows(mat canonical.CanonicalMaterialization) []map[string]any {
	return terraformstate.ResourceRows(mat)
}

func terraformStateOwningRepoIDValue(repoID string) any {
	return terraformstate.OwningRepoIDValue(repoID)
}

func wrapTerraformParts(parts []terraformstate.Part) []Statement {
	if len(parts) == 0 {
		return nil
	}
	statements := make([]Statement, 0, len(parts))
	for _, part := range parts {
		operation := OperationCanonicalUpsert
		if part.IsRetract {
			operation = OperationCanonicalRetract
		}
		statements = append(statements, Statement{
			Operation:  operation,
			Cypher:     part.Cypher,
			Parameters: part.Parameters,
			Drain:      part.Drain,
			DrainVar:   part.DrainVar,
		})
	}
	return statements
}
