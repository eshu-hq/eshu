// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
)

// Incident edge assertions for the incident-context read model (#6060,
// lane B S2). They moved here because the incident family tests now live in
// three packages — incident/model (response assembly), incident (HTTP
// surface), and incident/store (Postgres reads) — and a _test.go declaration
// in any one of them is unreachable from the others. They operate on the
// incident/model types so every leaf's tests pin the same evidence-path
// contract. The model package's own response test keeps local copies: an
// in-package (package model) test file cannot import this package without an
// import cycle, since this file imports the model package it would test.

// AssertIncidentEdge fails unless edges holds slot with label, returning the
// matched edge for further assertions.
func AssertIncidentEdge(
	t *testing.T,
	edges []model.IncidentContextEvidenceEdge,
	slot model.IncidentEvidenceSlot,
	label model.IncidentTruthLabel,
) *model.IncidentContextEvidenceEdge {
	t.Helper()
	for _, edge := range edges {
		if edge.Slot == slot {
			if edge.TruthLabel != label {
				t.Fatalf("edge %s truth_label = %q, want %q", slot, edge.TruthLabel, label)
			}
			return &edge
		}
	}
	t.Fatalf("missing edge for slot %s in %#v", slot, edges)
	return nil
}

// AssertIncidentMissing fails unless missing names slot with a non-blank
// reason.
func AssertIncidentMissing(
	t *testing.T,
	missing []model.IncidentMissingEvidence,
	slot model.IncidentEvidenceSlot,
) {
	t.Helper()
	for _, item := range missing {
		if item.Slot == slot {
			if item.Reason == "" {
				t.Fatalf("missing slot %s reason is blank", slot)
			}
			return
		}
	}
	t.Fatalf("missing evidence does not include slot %s: %#v", slot, missing)
}
