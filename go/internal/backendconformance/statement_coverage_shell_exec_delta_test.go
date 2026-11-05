// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// shellExecReadCallsite is the builder identity the coverage gate records
// for both shell-exec candidate-key reads: readShellCommandKeys issues the
// whole-repository read on a full generation and the file-scoped read on a
// delta generation.
const shellExecReadCallsite = "internal/storage/cypher/edge/writer/shell_exec.go:(*EdgeWriter).readShellCommandKeys"

// shellExecDeltaKeysStatement mirrors shellCommandCandidateKeysByFileCypher
// in go/internal/storage/cypher/edge/writer/shell_exec.go (whitespace
// folding is the gate's normalization, so layout need not match).
const shellExecDeltaKeysStatement = `UNWIND $file_paths AS file_path
MATCH (target:ShellCommand {path: file_path})
WHERE target.evidence_source = $evidence_source
RETURN DISTINCT target.uid AS key`

// TestStatementCoverageShellExecDeltaKeysExempt pins the B-7 exemption for
// the delta-scoped shell-exec candidate read. The #10022 delta-reader leg is
// the corpus's only git delta, and the git collector emits the shell-exec
// follow-up on every delta, so the read executes there; the corpus holds no
// ShellCommand node, so it returns zero rows on every backend. Without a
// manifest row the gate fails the callsite as always-empty (observed on
// neo4j in CI run 41953841855, the first run with the delta leg).
func TestStatementCoverageShellExecDeltaKeysExempt(t *testing.T) {
	t.Parallel()

	manifest, err := queryplan.LoadBuilderManifest("../queryplan/testdata/statement-builders.yaml")
	if err != nil {
		t.Fatalf("LoadBuilderManifest() error = %v", err)
	}
	records := map[string][]DifferentialRecord{"neo4j": {{
		Backend:  "neo4j",
		Callsite: shellExecReadCallsite,
		Fingerprint: DifferentialFingerprint{
			Statement:  shellExecDeltaKeysStatement,
			Parameters: `{"evidence_source":"parser/shell-exec","file_paths":["/repos/acme/golden-delta-reader/app/main.go"]}`,
		},
		RowCount: 0,
		Digest:   "empty-digest",
		Kind:     DifferentialRecordRead,
	}}}
	// The synthetic record set carries only this read, so unrelated manifest
	// findings (stale exemptions, never-executed builders) also surface here;
	// the assertion scopes to the delta read's text on this callsite.
	for _, failure := range ComputeStatementCoverage(manifest, records).Failures() {
		if strings.Contains(failure.ID, shellExecReadCallsite) && strings.Contains(failure.ID, "{path: file_path}") {
			t.Fatalf("Failures() contains %v: the corpus carries no ShellCommand nodes, so the delta read is empty by construction", failure)
		}
	}
}
