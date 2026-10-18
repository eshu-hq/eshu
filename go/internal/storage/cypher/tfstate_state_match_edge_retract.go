// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	terraformstate "github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/terraform"
)

func (w *CanonicalNodeWriter) terraformStateMatchesConfigEdgeRetractStatements(mat canonical.CanonicalMaterialization) []Statement {
	return wrapTerraformParts(terraformstate.TerraformStateMatchesConfigEdgeRetractParts(mat, w.batchSize))
}
