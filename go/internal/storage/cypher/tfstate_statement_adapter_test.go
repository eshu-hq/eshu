// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	terraformstate "github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/terraform"
)

func TestTerraformStatePartsAdaptWithoutChangingStatements(t *testing.T) {
	t.Parallel()

	mat := canonical.CanonicalMaterialization{
		ScopeID:         "scope",
		GenerationID:    "generation",
		FirstGeneration: false,
		TerraformStateResources: []canonical.TerraformStateResourceRow{{
			UID:              "resource",
			Address:          "aws_instance.web",
			ResourceType:     "aws_instance",
			OwningRepoID:     "repo",
			OwnershipOutcome: canonical.TerraformStateOwnershipResolved,
		}},
	}
	parts := terraformstate.BuildTerraformStateStatements(mat, 0, 500)
	statements := wrapTerraformParts(parts)
	if len(statements) != len(parts) {
		t.Fatalf("statement count = %d, part count = %d", len(statements), len(parts))
	}
	for i, part := range parts {
		wantOperation := OperationCanonicalUpsert
		if part.IsRetract {
			wantOperation = OperationCanonicalRetract
		}
		want := Statement{
			Operation:  wantOperation,
			Cypher:     part.Cypher,
			Parameters: part.Parameters,
			Drain:      part.Drain,
			DrainVar:   part.DrainVar,
		}
		if !reflect.DeepEqual(statements[i], want) {
			t.Fatalf("statement[%d] = %#v, want %#v", i, statements[i], want)
		}
	}

	var drainedRetract bool
	for _, stmt := range statements {
		if stmt.Operation == OperationCanonicalRetract && stmt.Drain && stmt.DrainVar == "" &&
			stmt.Parameters[StatementMetadataEntityLabelKey] == "MATCHES_STATE" {
			drainedRetract = true
		}
	}
	if !drainedRetract {
		t.Fatal("MATCHES_STATE relationship retract lost Drain=true with an empty DrainVar")
	}
}
