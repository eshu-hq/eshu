// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// genuineNoMatchPayloadSHA256 is the sha256 of the marshaled payload of
// impactWaitPendingDecision written with no impact_wait_outcome, measured on
// origin/main before #8956. A genuine no-match must keep this exact payload.
const genuineNoMatchPayloadSHA256 = "7c3cfcbb0fd0a5dc570a0877ae737c264cd59e98cdbec7d95e953924515d5c02"

func impactWaitPendingWrite(outcome string, decisions ...SecurityAlertReconciliationDecision) SecurityAlertReconciliationWrite {
	return SecurityAlertReconciliationWrite{
		IntentID:          "i",
		ScopeID:           "s",
		GenerationID:      "g",
		SourceSystem:      "security_alert",
		Decisions:         decisions,
		ImpactWaitOutcome: outcome,
	}
}

// impactWaitPendingDecision is the decision shape that waits on impact
// evidence: unmatched, owned dependency evidence, no impact evidence.
func impactWaitPendingDecision() SecurityAlertReconciliationDecision {
	return SecurityAlertReconciliationDecision{
		Provider:             "github_dependabot",
		ProviderAlertNumber:  7,
		PackageID:            "npm://x",
		Status:               SecurityAlertReconciliationUnmatched,
		DependencyEvidenceID: "dep-1",
		ReasonCode:           "impact_finding_missing",
	}
}

func writtenPayloads(t *testing.T, write SecurityAlertReconciliationWrite) ([]map[string]any, []string) {
	t.Helper()
	db := &fakeWorkloadIdentityExecer{}
	if _, err := (PostgresSecurityAlertReconciliationWriter{DB: db}).WriteSecurityAlertReconciliations(context.Background(), write); err != nil {
		t.Fatalf("WriteSecurityAlertReconciliations() error = %v", err)
	}
	rows := decodeBatchedFactCalls(t, db.execs)
	payloads := make([]map[string]any, 0, len(rows))
	raws := make([]string, 0, len(rows))
	for _, row := range rows {
		var payload map[string]any
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		payloads = append(payloads, payload)
		raws = append(raws, string(row.Payload))
	}
	return payloads, raws
}

func TestSecurityAlertReconciliationWriterPersistsTheGiveUpOutcome(t *testing.T) {
	t.Parallel()

	for _, outcome := range PersistedImpactWaitOutcomes() {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			matched := impactWaitPendingDecision()
			matched.ProviderAlertNumber = 8
			matched.Status = SecurityAlertReconciliationMatched
			matched.ImpactEvidenceID = "impact-1"
			providerOnly := impactWaitPendingDecision()
			providerOnly.ProviderAlertNumber = 9
			providerOnly.Status = SecurityAlertReconciliationProviderOnly
			providerOnly.DependencyEvidenceID = ""

			payloads, _ := writtenPayloads(t, impactWaitPendingWrite(outcome, impactWaitPendingDecision(), matched, providerOnly))
			if len(payloads) != 3 {
				t.Fatalf("rows = %d, want 3", len(payloads))
			}
			if got := payloads[0]["impact_wait_outcome"]; got != outcome {
				t.Fatalf("waiting decision impact_wait_outcome = %v, want %q", got, outcome)
			}
			for i, label := range map[int]string{1: "matched", 2: "provider_only"} {
				if got, ok := payloads[i]["impact_wait_outcome"]; ok {
					t.Fatalf("%s decision carries impact_wait_outcome %v; only a decision that waited on impact evidence may", label, got)
				}
			}
		})
	}
}

// TestSecurityAlertReconciliationWriterKeepsTheGenuineNoMatchPayload proves a
// write with no give-up outcome produces the pre-#8956 payload byte for byte.
func TestSecurityAlertReconciliationWriterKeepsTheGenuineNoMatchPayload(t *testing.T) {
	t.Parallel()

	payloads, raws := writtenPayloads(t, impactWaitPendingWrite("", impactWaitPendingDecision()))
	if _, ok := payloads[0]["impact_wait_outcome"]; ok {
		t.Fatalf("genuine no-match payload carries impact_wait_outcome: %s", raws[0])
	}
	sum := sha256.Sum256([]byte(raws[0]))
	if got := hex.EncodeToString(sum[:]); got != genuineNoMatchPayloadSHA256 {
		t.Fatalf("genuine no-match payload sha256 = %s, want %s (pre-#8956 bytes):\n%s", got, genuineNoMatchPayloadSHA256, raws[0])
	}
}

// TestSecurityAlertReconciliationWriterRejectsAnOutcomeOutsideTheClosedSet
// keeps the persisted set closed: the defer and settled outcomes, and any
// free text, are refused before any row is written.
func TestSecurityAlertReconciliationWriterRejectsAnOutcomeOutsideTheClosedSet(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{ImpactWaitDeferredInFlight, ImpactWaitProducerSettled, "gave_up"} {
		db := &fakeWorkloadIdentityExecer{}
		_, err := (PostgresSecurityAlertReconciliationWriter{DB: db}).WriteSecurityAlertReconciliations(
			context.Background(), impactWaitPendingWrite(outcome, impactWaitPendingDecision()))
		if err == nil || !strings.Contains(err.Error(), "impact wait outcome") {
			t.Fatalf("outcome %q: error = %v, want a closed-set rejection", outcome, err)
		}
		if len(db.execs) != 0 {
			t.Fatalf("outcome %q: exec calls = %d, want 0", outcome, len(db.execs))
		}
	}
}
