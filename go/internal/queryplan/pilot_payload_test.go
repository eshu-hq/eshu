// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPilotPayloadRequiresFullStatementSamples(t *testing.T) {
	manifest, artifact := pilotEvidenceFixture()
	artifact.Entries[0].Cases[0].Candidate.StatementResults = nil
	if err := ValidatePilotEvidence(manifest, &artifact); err == nil {
		t.Fatal("accepted projected identities without full statement result samples")
	}
}

func TestPilotPayloadRejectsWiderRowsWithSameIdentity(t *testing.T) {
	manifest, artifact := pilotEvidenceFixture()
	caseEvidence := &artifact.Entries[0].Cases[0]
	row := json.RawMessage(`[{"uid":"fixture-uid"}]`)
	for _, run := range []*PilotCaseRun{&caseEvidence.Base, &caseEvidence.Candidate} {
		run.StatementResults = []json.RawMessage{row, row, row, row}
	}
	if err := ValidatePilotEvidence(manifest, &artifact); err != nil {
		t.Fatalf("full rows at declared cap: %v", err)
	}
	caseEvidence.Candidate.StatementResults[3] = json.RawMessage(`[{"uid":"fixture-uid","extra":"wide"}]`)
	if err := ValidatePilotEvidence(manifest, &artifact); err == nil {
		t.Fatal("accepted wider captured row with unchanged identity")
	}
}

func TestPilotPayloadRejectsIncompleteAndNonRowSamples(t *testing.T) {
	manifest, artifact := pilotEvidenceFixture()
	run := &artifact.Entries[0].Cases[0].Candidate
	for _, raw := range []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(`{"uid":"fixture-uid"}`),
		json.RawMessage(`[null]`),
		json.RawMessage(`[{}]`),
		json.RawMessage(`[]`),
		json.RawMessage(`[{"uid":"fixture-uid"},null]`),
	} {
		run.StatementResults[3] = raw
		if err := ValidatePilotEvidence(manifest, &artifact); err == nil {
			t.Fatalf("accepted invalid full-row sample %s", raw)
		}
	}
	run.StatementResults = run.StatementResults[:3]
	if err := ValidatePilotEvidence(manifest, &artifact); err == nil {
		t.Fatal("accepted missing fourth timed sample")
	}
}

func TestPilotPayloadAllowsEmptyRowsWhenPlanAndOracleAgree(t *testing.T) {
	_, artifact := pilotEvidenceFixture()
	run := artifact.Entries[0].Cases[0].Candidate
	run.Result = json.RawMessage(`[]`)
	run.Plan = bytes.Replace(run.Plan, []byte(`"Actual Rows":1`), []byte(`"Actual Rows":0`), 1)
	for i := range run.StatementResults {
		run.StatementResults[i] = json.RawMessage(`[]`)
	}
	if violations := validatePilotStatementResults("empty", run, 2, queryKindSQLReadModel); len(violations) != 0 {
		t.Fatalf("legitimate zero-row samples rejected: %v", violations)
	}
}
