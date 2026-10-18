// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	terraformstate "github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/terraform"
)

const (
	terraformStateResourceMigrationCypher                    = terraformstate.TerraformStateResourceMigrationCypher
	canonicalTerraformStateResourceRetractCurrentLabelCypher = terraformstate.TerraformStateResourceRetractCurrentLabelCypher
	canonicalTerraformStateResourceRetractLegacyLabelCypher  = terraformstate.TerraformStateResourceRetractLegacyLabelCypher
	terraformStateResourceStaleContentPropRemoveCypher       = terraformstate.TerraformStateResourceStaleContentPropRemoveCypher
)

func (w *CanonicalNodeWriter) terraformStateResourceStaleContentPropRemoveStatements(mat canonical.CanonicalMaterialization) []Statement {
	return wrapTerraformParts(terraformstate.TerraformStateResourceStaleContentPropRemoveParts(mat, w.batchSize))
}

func (w *CanonicalNodeWriter) terraformStateResourceRetractStatements(mat canonical.CanonicalMaterialization) []Statement {
	return wrapTerraformParts(terraformstate.TerraformStateResourceRetractParts(mat))
}
