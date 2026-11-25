// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
)

// TestClaimGateAndCheckpointShareOneUndrainedSet pins the eshu-hq/eshu#11380
// single-helper contract: the batch-claim gate CTE and the checkpoint family
// read project their own columns over the same undrained-generation row
// source, verbatim. The core names the set both sides mean by "undrained
// generation of an incomplete year" (every older live window of the year's
// baseline family); neither side may restate its fences. A re-split that
// inlines a divergent copy on either side fails here before it can drift the
// two definitions apart again the way eshu-hq/eshu#11366 did.
func TestClaimGateAndCheckpointShareOneUndrainedSet(t *testing.T) {
	t.Parallel()

	core := vulnerabilitystore.NVDBaselineUndrainedGenerationRowsSQL
	if strings.TrimSpace(core) == "" {
		t.Fatal("undrained-generation row source is blank; the gate and the family read share nothing")
	}
	if !strings.Contains(claimReducerWorkBatchQuery, core) {
		t.Fatal("batch claim no longer embeds the shared undrained-generation row source verbatim; " +
			"the gate CTE re-split its definition")
	}
	//nolint:gocritic // argOrder: the family query is the haystack and the shared core the needle; the heuristic reads the qualified query name as the shorter match.
	if !strings.Contains(vulnerabilitystore.NVDBaselineUndrainedGenerationsQuery, core) {
		t.Fatal("checkpoint family query no longer embeds the shared undrained-generation row source verbatim; " +
			"the probe path re-split its definition")
	}
}
