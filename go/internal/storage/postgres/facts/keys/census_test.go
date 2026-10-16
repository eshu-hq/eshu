// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factkeys

import (
	"os"
	"strings"
	"testing"
)

const operatorRunbookPath = "../../../../../../docs/public/operate/3am-operator-runbook.md"

// TestOperatorRunbookQuotesTheShippedSQL keeps the 3 AM runbook's census and
// cleanup statements byte-identical to the constants, so an operator never
// runs a drifted copy (for example a fact_id-only anti-join).
func TestOperatorRunbookQuotesTheShippedSQL(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile(operatorRunbookPath)
	if err != nil {
		t.Fatalf("read runbook: %v", err)
	}
	for name, statement := range map[string]string{
		"OrphanCensusSQL":  OrphanCensusSQL,
		"OrphanCleanupSQL": OrphanCleanupSQL,
	} {
		if !strings.Contains(string(body), "```sql\n"+statement+"```") {
			t.Errorf("runbook does not quote %s verbatim in a sql fence", name)
		}
	}
}
