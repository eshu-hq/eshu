// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestPilotAlternateProofRejectsEmptyPlan(t *testing.T) {
	manifest, artifact := pilotEvidenceFixture()
	for _, raw := range []string{`null`, `{}`, `{"plan":{},"work":{"query_count":1,"shared_blocks":2}}`} {
		t.Run(raw, func(t *testing.T) {
			run := artifact.Entries[0].Cases[0].Candidate
			run.Plan = nil
			run.PlanUnavailable = "backend does not expose plans"
			run.AlternateProof = json.RawMessage(raw)
			if got := validatePilotCaseRun("candidate", run, run.Result, manifest.Entries[0].Contract.Budget, "sql-runner"); len(got) == 0 {
				t.Fatal("accepted missing plan with empty alternate proof")
			}
		})
	}
}

func TestPilotAlternateProofBindsPlanProvenance(t *testing.T) {
	manifest, artifact := pilotEvidenceFixture()
	run := artifact.Entries[0].Cases[0].Candidate
	run.Plan, run.Work = nil, nil
	run.PlanUnavailable = "backend does not expose plans"
	plan := json.RawMessage(`{"operator":"indexed lookup","keys":["uid"]}`)
	proof := func(producer, hash, work string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"plan":%s,"producer":%q,"artifact_sha256":%q,"work":%s}`, plan, producer, hash, work))
	}
	work := `{"query_count":1,"shared_blocks":2}`
	run.AlternateProof = proof("independent-probe", PilotJSONSHA256(plan), work)
	if got := validatePilotCaseRun("candidate", run, run.Result, manifest.Entries[0].Contract.Budget, "sql-runner"); len(got) != 0 {
		t.Fatalf("valid alternate: %v", got)
	}
	for _, raw := range []json.RawMessage{
		proof("independent-probe", "bad", work),
		proof("sql-runner", PilotJSONSHA256(plan), work),
		proof("", PilotJSONSHA256(plan), work),
		proof("independent-probe", PilotJSONSHA256(plan), `{}`),
		proof("independent-probe", PilotJSONSHA256(plan), `{"query_count":1}`),
	} {
		run.AlternateProof = raw
		if got := validatePilotCaseRun("candidate", run, run.Result, manifest.Entries[0].Contract.Budget, "sql-runner"); len(got) == 0 {
			t.Fatalf("accepted invalid alternate %s", raw)
		}
	}
}
