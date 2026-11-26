// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func scheduledProofGate(id string) cigates.Gate {
	gate := replayProofGate(id, "bash scripts/verify-"+id+".sh", id+".yml")
	gate.Tier = cigates.TierCIHeavy
	gate.Layer = cigates.LayerTruth
	gate.Blocking = false
	return gate
}

func jobsAreScheduled(cigates.Gate) (bool, error)    { return true, nil }
func jobsAreNotScheduled(cigates.Gate) (bool, error) { return false, nil }

func TestRunGateCoversSurfaceWithScheduledCIHeavyProofGate(t *testing.T) {
	gate := scheduledProofGate("golden-corpus-gate-neo4j")
	in := proofOffInputs("golden-corpus-gate-neo4j", gate)
	in.JobScheduled = jobsAreScheduled

	cov, rep, gr := RunGate(in)
	if gr.Failed() {
		t.Fatalf("an active scheduled ci-heavy proof gate must pass: %+v", gr)
	}
	if cov.Surfaces[0].Status != StatusCovered || rep.Totals.Covered != 1 || rep.Totals.ProofOff != 0 {
		t.Fatalf("status = %v totals = %+v, want covered and proof_off=0", cov.Surfaces[0].Status, rep.Totals)
	}
	if errs := ValidateRequiredProofGates(in.Manifest, AuthzProofLedger{}, in.ProofGates, in.JobDisabled, in.JobScheduled); len(errs) != 0 {
		t.Fatalf("ValidateRequiredProofGates returned unexpected errors: %v", errs)
	}
}

func TestValidateRequiredProofGatesRejectsInvalidScheduledPostures(t *testing.T) {
	cases := map[string]struct {
		mutate  func(*cigates.Gate)
		checker JobScheduledFunc
		want    string
	}{
		"nil checker fails closed": {
			mutate:  func(*cigates.Gate) {},
			checker: nil,
			want:    "scheduled workflow cannot be verified",
		},
		"erroring checker fails closed": {
			mutate:  func(*cigates.Gate) {},
			checker: func(cigates.Gate) (bool, error) { return false, errors.New("no job named corpus-gate") },
			want:    "scheduled workflow cannot be verified",
		},
		"unscheduled workflow fails closed": {
			mutate:  func(*cigates.Gate) {},
			checker: jobsAreNotScheduled,
			want:    "is not scheduled to run",
		},
		"pre-pr advisory tier rejected even with scheduled checker": {
			mutate:  func(g *cigates.Gate) { g.Tier = cigates.TierPrePR },
			checker: jobsAreScheduled,
			want:    "is not blocking",
		},
		"unmarked manual tier rejected even with scheduled checker": {
			mutate:  func(g *cigates.Gate) { g.Tier = cigates.TierManual },
			checker: jobsAreScheduled,
			want:    "is not blocking",
		},
		"secondary layer rejected even on ci-heavy scheduled workflow": {
			mutate:  func(g *cigates.Gate) { g.Layer = cigates.LayerSecondary },
			checker: jobsAreScheduled,
			want:    "is not blocking",
		},
		"missing local command rejected on scheduled gate": {
			mutate:  func(g *cigates.Gate) { g.Local = nil },
			checker: jobsAreScheduled,
			want:    "has no local command",
		},
		"disabled marker on ci-heavy scheduled gate rejected": {
			mutate: func(g *cigates.Gate) {
				g.Disabled = &cigates.Disabled{Reason: "off", Issue: "#11481"}
			},
			checker: jobsAreScheduled,
			want:    "manual and non-blocking",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gate := scheduledProofGate("golden-corpus-gate-neo4j")
			tc.mutate(&gate)
			in := proofOffInputs("golden-corpus-gate-neo4j", gate)
			in.JobScheduled = tc.checker

			cov, rep, gr := RunGate(in)
			if !gr.Failed() {
				t.Fatalf("RunGate must fail for case %q", name)
			}
			if cov.Surfaces[0].Status != StatusUnresolved || rep.Totals.ProofOff != 0 {
				t.Fatalf("status = %v, proof_off = %d, want unresolved and 0", cov.Surfaces[0].Status, rep.Totals.ProofOff)
			}
			errs := ValidateRequiredProofGates(in.Manifest, AuthzProofLedger{}, in.ProofGates, in.JobDisabled, tc.checker)
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), tc.want) {
				t.Fatalf("ValidateRequiredProofGates errors = %v, want 1 error containing %q", errs, tc.want)
			}
		})
	}
}
