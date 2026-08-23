// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"errors"
	"strings"
	"testing"
)

// TestScanShellLineParsesTheAcceptedGrammar pins the grammar
// requiredworkflow_shell.go documents, case by case. The publisher validators
// read arms through this scanner, so a shape it gets wrong is a shape the
// #6075/#6189 contract can be defeated with.
func TestScanShellLineParsesTheAcceptedGrammar(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line       string
		state      string
		assigned   bool
		terminated bool
	}{
		"plain assignment": {
			line: " state=error ;;", state: "error", assigned: true, terminated: true,
		},
		"last assignment wins": {
			line: " state=error; state=success ;;", state: "success", assigned: true, terminated: true,
		},
		"assignment prefix sets both names": {
			line: " description=x state=success ;;", state: "success", assigned: true, terminated: true,
		},
		"a command ends the assignment prefix": {
			line: " echo state=success ;;", terminated: true,
		},
		"a quoted state= token is text, not an assignment": {
			line:  " state=failure; description='cancelled: publishes state=error and blocks' ;;",
			state: "failure", assigned: true, terminated: true,
		},
		"a separator inside quotes does not start a statement": {
			line:  " state=error; description='never ran; re-run it (no gate failed)' ;;",
			state: "error", assigned: true, terminated: true,
		},
		"a terminator inside quotes does not end the arm": {
			line: " description='a;;b'; state=failure ;;", state: "failure", assigned: true, terminated: true,
		},
		"a hash inside quotes is not a comment": {
			line: " description='see #6218'; state=failure ;;", state: "failure", assigned: true, terminated: true,
		},
		"a hash starting a word is a comment": {
			line: " state=error # state=success", state: "error", assigned: true,
		},
		"a hash inside a word is text": {
			line: " state=PR#6218 ;;", state: "PR#6218", assigned: true, terminated: true,
		},
		"a fully quoted assignment is a command name": {
			line: ` "state=success" ;;`, terminated: true,
		},
		"double quotes keep their contents literal": {
			line: ` state="error" ;;`, state: "error", assigned: true, terminated: true,
		},
		"an unterminated line has no terminator": {
			line: " state=error", state: "error", assigned: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			statements, terminated, err := scanShellLine(tc.line)
			if err != nil {
				t.Fatalf("scanShellLine(%q) = %v; the accepted grammar must parse", tc.line, err)
			}
			if terminated != tc.terminated {
				t.Errorf("terminated = %v, want %v", terminated, tc.terminated)
			}
			state, assigned := effectiveStateAssignment(statements)
			if assigned != tc.assigned || state != tc.state {
				t.Errorf("effective state = (%q, %v), want (%q, %v)", state, assigned, tc.state, tc.assigned)
			}
		})
	}
}

// TestScanShellLineRefusesWhatItDoesNotModel is the other half of the design:
// every construct the grammar leaves out returns an error instead of a guess.
// #6194 is the record of what guessing costs -- nine review rounds extending a
// textual bash model one character class at a time, never converging.
func TestScanShellLineRefusesWhatItDoesNotModel(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"command substitution":              " state=$(echo error) ;;",
		"backtick substitution":             " state=`echo error` ;;",
		"backslash escape":                  ` state=err\or ;;`,
		"line continuation":                 ` state=error \`,
		"unterminated single quote":         " state='error ;;",
		"unterminated double quote":         ` state="error ;;`,
		"subshell grouping":                 " (state=error) ;;",
		"redirection":                       " state=error > /dev/null ;;",
		"escape inside double quotes":       ` description="a \" b"; state=error ;;`,
		"substitution inside double quotes": ` description="$(id)"; state=error ;;`,
	}
	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			statements, terminated, err := scanShellLine(line)
			if err == nil {
				t.Fatalf("scanShellLine(%q) parsed as %v (terminated=%v); shell this scanner does not model "+
					"must be reported, not guessed at", line, statements, terminated)
			}
			if !errors.Is(err, errUnparseableShell) {
				t.Fatalf("scanShellLine(%q) = %v; want an errUnparseableShell", line, err)
			}
		})
	}
}

// TestAggregateCodeArmStopsAtItsOwnTerminator guards the arm boundary. An arm
// that swallowed the arms below it would read their assignments as its own,
// which is how the #6189 review's comment attack got the guard's `state=error`
// to answer for a deleted cancelled arm.
func TestAggregateCodeArmStopsAtItsOwnTerminator(t *testing.T) {
	t.Parallel()

	run := aggregateCaseHeader + `
  11) exit 0 ;;
  13) state=error ;;
  *) state=failure ;;
esac
`
	arm, ok, err := aggregateCodeArm(run, awaitExitGateCancelledCode)
	if err != nil || !ok {
		t.Fatalf("aggregateCodeArm = (ok %v, err %v); the 13) arm is right there", ok, err)
	}
	if state, assigned := effectiveStateAssignment(arm); !assigned || state != "error" {
		t.Fatalf("effective state = (%q, %v); the arm must not read past its own `;;`", state, assigned)
	}
}

// TestAggregateCodeArmReportsUnparseableShell proves the refusal reaches the
// caller rather than being flattened into "no arm" or "assigns nothing" --
// either of which would be a silent pass for the still-running check.
func TestAggregateCodeArmReportsUnparseableShell(t *testing.T) {
	t.Parallel()

	run := aggregateCaseHeader + `
  13) state=$(echo error) ;;
esac
`
	_, ok, err := aggregateCodeArm(run, awaitExitGateCancelledCode)
	if err == nil {
		t.Fatal("an arm built from unmodelled shell must be reported")
	}
	if !ok {
		t.Fatal("the arm exists; reporting it as absent would name the wrong defect")
	}
	if !strings.Contains(err.Error(), "13) arm") {
		t.Fatalf("err = %v; the message must name which arm could not be parsed", err)
	}
}
