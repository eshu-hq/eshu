// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"slices"
	"testing"
)

// TestSchemaIndexesMixedWriterEvidenceSource pins the two indexes the infra
// resource aggregate read (#6793) relies on: the graph reads only the Terraform
// state projector's TerraformModule and TerraformOutput nodes, through
// `WHERE n.evidence_source = $graph_writer_evidence_source`, and that predicate
// must be an index seek, not a label scan.
func TestSchemaIndexesMixedWriterEvidenceSource(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"CREATE INDEX tf_module_evidence_source IF NOT EXISTS FOR (m:TerraformModule) ON (m.evidence_source)",
		"CREATE INDEX tf_output_evidence_source IF NOT EXISTS FOR (o:TerraformOutput) ON (o.evidence_source)",
	} {
		if !slices.Contains(schemaPerformanceIndexes, want) {
			t.Fatalf("schemaPerformanceIndexes missing %q", want)
		}
	}
}
