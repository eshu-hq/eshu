// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Shared verdict-key vector (eshu-hq/eshu#10732, collector#158). The window
// and baseline checkpoint verdicts a satellite stamps on the
// vulnerability.source_snapshot payload are read here by exact key name. The
// vector lives in eshu-sdk behind fixturepack.NVDVerdictKeysGolden(), and the
// satellite repo replays the same bytes against its constants and builders.
// A key rename or a changed verdict rule on either side therefore fails both
// suites. Core consumes it through the pinned eshu-sdk/factschema version:
// there is no local copy.

// nvdVerdictKeysVectorSHA256 is the SHA-256 of the pinned vector bytes. The
// satellite repo pins the same digest, so a vector change fails both suites
// until each side re-reads it. Bumping the SDK pin moves the vector, and the
// digest moves with it.
const nvdVerdictKeysVectorSHA256 = "fdb456f8bac262f62374514e041663d8a771e3f5993c9a63fc759815c9b670f9"

type nvdVerdictVector struct {
	SnapshotFactKind string                  `json:"snapshot_fact_kind"`
	Window           nvdVerdictVectorSection `json:"window"`
	Baseline         nvdVerdictVectorSection `json:"baseline"`
}

type nvdVerdictVectorSection struct {
	ScopePrefix string                 `json:"scope_prefix"`
	Keys        []nvdVerdictVectorKey  `json:"keys"`
	Cases       []nvdVerdictVectorCase `json:"cases"`
}

type nvdVerdictVectorKey struct {
	Field  string `json:"field"`
	Name   string `json:"name"`
	ReadBy string `json:"read_by"`
}

type nvdVerdictVectorCase struct {
	Name    string             `json:"name"`
	ScopeID string             `json:"scope_id"`
	Payload map[string]any     `json:"payload"`
	Expect  nvdVerdictExpected `json:"expect"`
}

// nvdVerdictExpected is a case's expect block: the commit outcome core's
// reader reaches and the values the armed writer carries (0, 0 and false
// when nothing is armed).
type nvdVerdictExpected struct {
	Outcome        string `json:"outcome"`
	Reason         string `json:"reason"`
	Class          string `json:"class"`
	AdvanceTo      int64  `json:"advance_to"`
	Head           int64  `json:"head"`
	YearComplete   bool   `json:"year_complete"`
	AdvanceToCVEID string `json:"advance_to_cve_id"`
	YearGenerated  int64  `json:"year_generated_unix_us"`
}

// loadNVDVerdictVector decodes the vector after checking its digest. Payload
// numbers decode as float64, the shape the claim transport hands the reader.
func loadNVDVerdictVector(t *testing.T) nvdVerdictVector {
	t.Helper()
	raw := fixturepack.NVDVerdictKeysGolden()
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != nvdVerdictKeysVectorSHA256 {
		t.Fatalf("shared verdict-key vector sha256 = %x, want %s: the SDK pin moved, so re-read the vector, replay it, and update the digest with it",
			sum, nvdVerdictKeysVectorSHA256)
	}
	var vector nvdVerdictVector
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatalf("decode shared verdict-key vector: %v", err)
	}
	if vector.SnapshotFactKind != nvdWindowSnapshotFactKind {
		t.Fatalf("vector snapshot_fact_kind = %q, want %q", vector.SnapshotFactKind, nvdWindowSnapshotFactKind)
	}
	return vector
}

// TestNVDVerdictKeyConstantsMatchSharedVector fails when a key constant the
// reader uses drifts from the vector, or when the vector marks a key as read
// by core that the reader has no constant for.
func TestNVDVerdictKeyConstantsMatchSharedVector(t *testing.T) {
	t.Parallel()

	vector := loadNVDVerdictVector(t)
	sections := []struct {
		name      string
		section   nvdVerdictVectorSection
		constants map[string]string
	}{
		{"window", vector.Window, map[string]string{
			"advance_to":          nvdWindowAdvanceToPayloadKey,
			"head":                nvdWindowHeadPayloadKey,
			"should_advance":      nvdWindowShouldAdvancePayloadKey,
			"time_anomaly_reason": nvdWindowAnomalyPayloadKey,
		}},
		{"baseline", vector.Baseline, map[string]string{
			"advance_to":        nvdBaselineAdvanceToPayloadKey,
			"year_sha256":       nvdBaselineYearSHAPayloadKey,
			"year_complete":     nvdBaselineYearCompletePayloadKey,
			"should_advance":    nvdBaselineShouldAdvancePayloadKey,
			"advance_to_cve_id": nvdBaselineAdvanceToCVEIDPayloadKey,
			"year_generated":    nvdBaselineYearGeneratedPayloadKey,
		}},
	}
	for _, tc := range sections {
		byField := map[string]nvdVerdictVectorKey{}
		for _, key := range tc.section.Keys {
			byField[key.Field] = key
			if key.ReadBy != "core" {
				continue
			}
			if _, ok := tc.constants[key.Field]; !ok {
				t.Errorf("%s: vector key %q (%s) is read by core, but the reader has no constant for it", tc.name, key.Field, key.Name)
			}
		}
		for field, constant := range tc.constants {
			key, ok := byField[field]
			if !ok {
				t.Errorf("%s: reader constant %q for field %q has no vector key", tc.name, constant, field)
				continue
			}
			if key.Name != constant {
				t.Errorf("%s: field %q: reader constant %q, vector name %q", tc.name, field, constant, key.Name)
			}
		}
	}
}

// TestNVDWindowVerdictReplaysSharedVector runs every window case through the
// reader that arms the cursor writer and requires the armed values to match
// the case's expect block. An outcome outside the window's closed set fails,
// so a case added upstream cannot go unreplayed.
func TestNVDWindowVerdictReplaysSharedVector(t *testing.T) {
	t.Parallel()

	vector := loadNVDVerdictVector(t)
	if len(vector.Window.Cases) == 0 {
		t.Fatal("vector carries no window cases")
	}
	source := mustNewSource(t, &recordingRunner{}, nil)
	for _, vc := range vector.Window.Cases {
		t.Run(vc.Name, func(t *testing.T) {
			t.Parallel()

			item := testWorkItem()
			item.ScopeID = vc.ScopeID
			result := completeResult(item)
			envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(vc.Payload)}
			collected := source.collectedGeneration(item, result, envelopes, "")
			switch vc.Expect.Outcome {
			case "held":
				if collected.StateWriter != nil {
					t.Fatalf("window case %q armed the cursor writer, vector expects held", vc.Name)
				}
			case "advanced":
				if collected.StateWriter == nil {
					t.Fatalf("window case %q armed no cursor writer, vector expects advanced", vc.Name)
				}
				recorder := &recordingCommitExecutor{}
				if err := collected.StateWriter(context.Background(), recorder); err != nil {
					t.Fatalf("StateWriter() error = %v", err)
				}
				if len(recorder.args) != 1 || len(recorder.args[0]) < 3 {
					t.Fatalf("StateWriter() args = %v, want one statement with instance, advance-to, head", recorder.args)
				}
				args := recorder.args[0]
				if args[1] != vc.Expect.AdvanceTo || args[2] != vc.Expect.Head {
					t.Fatalf("window case %q armed advance_to=%v head=%v, vector expects %d and %d",
						vc.Name, args[1], args[2], vc.Expect.AdvanceTo, vc.Expect.Head)
				}
			default:
				t.Fatalf("window case %q expects outcome %q, outside the replay's closed set", vc.Name, vc.Expect.Outcome)
			}
		})
	}
}

// TestNVDBaselineVerdictVectorReplaysThroughCommitPolicy runs every baseline case through
// the commit policy and requires the decision to match the case's expect
// block: armed checkpoint values on advanced, the bounded not-covered reason
// on not_covered, the failure class on refused, and silence on held.
func TestNVDBaselineVerdictVectorReplaysThroughCommitPolicy(t *testing.T) {
	t.Parallel()

	vector := loadNVDVerdictVector(t)
	if len(vector.Baseline.Cases) == 0 {
		t.Fatal("vector carries no baseline cases")
	}
	for _, vc := range vector.Baseline.Cases {
		t.Run(vc.Name, func(t *testing.T) {
			t.Parallel()

			logs := &bytes.Buffer{}
			policy, err := NewNVDBaselineCommitPolicy("scorecard-instance", nil, slog.New(slog.NewJSONHandler(logs, nil)))
			if err != nil {
				t.Fatalf("NewNVDBaselineCommitPolicy() error = %v", err)
			}
			item := testWorkItem()
			item.ScopeID = vc.ScopeID
			envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(vc.Payload)}
			decision, err := policy.CommitPolicy(item, time.Now().UTC(), envelopes)
			records := logRecords(t, logs)
			assertBaselineDecisionMatchesVector(t, vc, decision, err, records)
		})
	}
}

func assertBaselineDecisionMatchesVector(
	t *testing.T,
	vc nvdVerdictVectorCase,
	decision ClaimCommitDecision,
	err error,
	records []map[string]any,
) {
	t.Helper()
	if !decision.Handled {
		t.Fatalf("baseline case %q was not handled by the baseline policy", vc.Name)
	}
	expect := vc.Expect
	if expect.Outcome == "refused" {
		var refusal *CommitPolicyRefusal
		if !errors.As(err, &refusal) || refusal.Class != expect.Class {
			t.Fatalf("baseline case %q error = %v, vector expects a refusal of class %q", vc.Name, err, expect.Class)
		}
		if decision.StateWriter != nil {
			t.Fatalf("baseline case %q refused but armed a checkpoint writer", vc.Name)
		}
		return
	}
	if err != nil {
		t.Fatalf("baseline case %q error = %v, vector expects %q", vc.Name, err, expect.Outcome)
	}
	switch expect.Outcome {
	case "held", "not_covered":
		if decision.StateWriter != nil {
			t.Fatalf("baseline case %q armed a checkpoint writer, vector expects %q", vc.Name, expect.Outcome)
		}
		assertNotCoveredLog(t, vc, records)
	case "advanced":
		if decision.StateWriter == nil {
			t.Fatalf("baseline case %q armed no checkpoint writer, vector expects advanced", vc.Name)
		}
		if len(records) != 0 {
			t.Fatalf("baseline case %q logged %v, vector expects a silent advance", vc.Name, records)
		}
		recorder := &affectedCommitExecutor{}
		if err := decision.StateWriter(context.Background(), recorder); err != nil {
			t.Fatalf("StateWriter() error = %v", err)
		}
		if len(recorder.args) < 1 || len(recorder.args[len(recorder.args)-1]) < 8 {
			t.Fatalf("StateWriter() args = %v, want one checkpoint statement", recorder.args)
		}
		args := recorder.args[len(recorder.args)-1]
		if args[6] != expect.AdvanceToCVEID || args[7] != expect.YearGenerated {
			t.Fatalf("baseline case %q armed cve=%v generated=%v, vector expects %q and %d", vc.Name, args[6], args[7], expect.AdvanceToCVEID, expect.YearGenerated)
		}
		if args[2] != expect.AdvanceTo || args[4] != expect.YearComplete {
			t.Fatalf("baseline case %q armed resume=%v complete=%v, vector expects %d and %v",
				vc.Name, args[2], args[4], expect.AdvanceTo, expect.YearComplete)
		}
	default:
		t.Fatalf("baseline case %q expects outcome %q, outside the replay's closed set", vc.Name, expect.Outcome)
	}
}

// assertNotCoveredLog requires exactly one not-covered WARN carrying the
// case's reason for a not_covered case, and no log at all for a held one.
func assertNotCoveredLog(t *testing.T, vc nvdVerdictVectorCase, records []map[string]any) {
	t.Helper()
	if vc.Expect.Outcome == "held" {
		if len(records) != 0 {
			t.Fatalf("baseline case %q logged %v, vector expects a silent hold", vc.Name, records)
		}
		return
	}
	if len(records) != 1 || records[0]["reason"] != vc.Expect.Reason {
		t.Fatalf("baseline case %q logs = %v, vector expects one WARN with reason %q", vc.Name, records, vc.Expect.Reason)
	}
}
