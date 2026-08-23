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
		"a permitted command assigns nothing": {
			line: " echo state=success ;;", terminated: true,
		},
		"exit assigns nothing": {
			line: " exit 0 ;;", terminated: true,
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
			state, assigned, err := effectiveStateAssignment(statements)
			if err != nil {
				t.Fatalf("effectiveStateAssignment(%q) = %v; the accepted grammar must be judged", tc.line, err)
			}
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
		// `$'failure'` is bash's ANSI-C quoting and evaluates to `failure`.
		// Read literally it is `$failure`, which is neither "failure" nor
		// "error", so the still-running check saw an arm assigning something
		// harmless while bash published the collapse #6075 removed
		// (#6218 review round 3).
		"ansi-c quoting":  " state=$'failure' ;;",
		"locale quoting":  ` state=$"failure" ;;`,
		"ansi-c mid-word": ` description='x'; state=pre$'fix' ;;`,
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
	state, assigned, err := effectiveStateAssignment(arm)
	if err != nil {
		t.Fatalf("effectiveStateAssignment = %v; this arm is inside the accepted grammar", err)
	}
	if !assigned || state != "error" {
		t.Fatalf("effective state = (%q, %v); the arm must not read past its own `;;`", state, assigned)
	}
}

// TestEffectiveStateAssignmentRefusesStatementsItCannotAccountFor is the
// #6218-round-3 regression guard. Each line below scans cleanly -- the lexer
// has no complaint -- and each one is a statement whose effect on `state` this
// validator cannot work out from the text. The rule that shipped before it
// (read the leading run of assignment words, stop at the first word that is
// not one) reported every one of them as assigning NOTHING, and an arm that
// assigns nothing passes the still-running check. So `export state=failure` in
// the 11) arm published `failure` for gates that had merely not finished --
// the exact collapse #6075 removed -- with the registry gate green.
//
// The fix is a whitelist rather than another entry on a list of builtins to
// watch for: the words that can set a variable have no end, and enumerating
// them one bypass at a time is #6194.
func TestEffectiveStateAssignmentRefusesStatementsItCannotAccountFor(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"export makes the assignment persist":  " export state=failure; description='still running' ;;",
		"a compound command hides the arm":     " if true; then state=failure; fi ;;",
		"declare is an assignment builtin too": " declare state=failure ;;",
		"eval can assign anything":             " eval state=failure ;;",
		// Bash keeps `state=error` only for the duration of `echo` here, so
		// reading it as the arm's verdict is wrong in the other direction:
		// the arm would be credited with a value bash does not leave behind.
		"an assignment prefixing a command does not persist": " state=error echo hi ;;",
		// The word is a command name, not an assignment, because the `=` came
		// through quoted -- so the arm runs a command this validator knows
		// nothing about rather than setting anything.
		"a fully quoted assignment is a command name": ` "state=success" ;;`,
		"an unlisted command":                         " printf state=error ;;",
	}
	for name, line := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			statements, _, err := scanShellLine(line)
			if err != nil {
				t.Fatalf("scanShellLine(%q) = %v; this line is inside the lexer's grammar, the "+
					"refusal belongs to the statement grammar", line, err)
			}
			state, assigned, err := effectiveStateAssignment(statements)
			if err == nil {
				t.Fatalf("effectiveStateAssignment(%q) = (%q, %v); a statement whose effect on `state` "+
					"cannot be read from the text must be reported, not treated as assigning nothing",
					line, state, assigned)
			}
			if !errors.Is(err, errUnparseableShell) {
				t.Fatalf("effectiveStateAssignment(%q) = %v; want an errUnparseableShell", line, err)
			}
		})
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

// TestReferencesShellVariableDoesNotMatchALongerName keeps the `gh api`
// binding check honest: `$statement` is not `$state`, and accepting it would
// let an unbound publisher pass.
func TestReferencesShellVariableDoesNotMatchALongerName(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		`"${state}"`:  true,
		`$state`:      true,
		`"$state"`:    true,
		`$statement`:  false,
		`success`:     false,
		`"${states}"`: false,
	}
	for value, want := range tests {
		if got := referencesShellVariable(value, "state"); got != want {
			t.Errorf("referencesShellVariable(%q) = %v, want %v", value, got, want)
		}
	}
}
