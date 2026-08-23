// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"strings"
)

// Arm reader for the required-gates terminal publisher's AGGREGATE_CODE case
// block, split out of requiredworkflow_publisher.go to keep that file under
// the 500-line cap. It sits between the word-level lexer in
// requiredworkflow_shell.go and the contract validators in
// requiredworkflow_publisher.go: this file locates one arm and says what that
// arm leaves in `state`, and it refuses -- loudly -- anything outside the
// statement grammar effectiveStateAssignment documents.

// aggregateCaseHeader opens the terminal publisher's AGGREGATE_CODE branch.
// Everything the arm locator reads starts after it, so an assignment on an
// earlier branch -- the PENDING_OUTCOME guard's own `state=error`, above the
// case -- can never be mistaken for an arm's.
const aggregateCaseHeader = `case "${AGGREGATE_CODE}" in`

// aggregateCodeArm parses the AGGREGATE_CODE case arm for one exit code into
// its statements, stopping at the arm's `;;` terminator. found is false when
// no such arm exists; err is non-nil when the arm is built from shell outside
// the grammar requiredworkflow_shell.go accepts, which callers report rather
// than judge.
//
// The arm is located STRUCTURALLY -- a line inside the case block that begins
// with `<code>)` -- rather than by searching the step for the substring
// "<code>)". The substring is defeatable, and for the cancelled-gate arm it
// was defeatable in the fail-OPEN direction (#6189 review). Ordinary prose
// produces the marker: a comment reading "the cancelled-gate outcome (13) is
// handled elsewhere" contains "13)". A locator that matched there extracted
// the region from the comment to the next `;;`, which spans the
// PENDING_OUTCOME guard's `state=error` and the `0)` arm, so "the arm must
// publish state=error" was satisfied by an assignment on another branch --
// while the real arm was deleted, or reverted to `state=failure`, underneath.
// Skipping comment lines and requiring the marker to START a line closes both.
//
// The `;;` that ends the arm is found by the scanner, not by a substring
// search: `;;` inside a quoted description is text, and truncating there
// dropped every assignment after it -- which reads as an arm that assigns
// nothing, and an arm that assigns nothing passes the still-running check.
func aggregateCodeArm(run string, code int) ([]shellStatement, bool, error) {
	header := strings.Index(run, aggregateCaseHeader)
	if header < 0 {
		return nil, false, nil
	}
	marker := fmt.Sprintf("%d)", code)
	lines := strings.Split(run[header+len(aggregateCaseHeader):], "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "esac" {
			// Past the end of the case block; anything below it is other
			// code, not an arm.
			return nil, false, nil
		}
		if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, marker) {
			continue
		}
		statements, err := scanArmBody(append([]string{strings.TrimPrefix(trimmed, marker)}, lines[i+1:]...))
		if err != nil {
			return nil, true, fmt.Errorf("%s arm: %w", marker, err)
		}
		return statements, true, nil
	}
	return nil, false, nil
}

// scanArmBody parses an arm's lines up to its `;;` terminator, or to the
// `esac` that closes the case block when the arm has no terminator.
func scanArmBody(lines []string) ([]shellStatement, error) {
	var statements []shellStatement
	for _, line := range lines {
		if strings.TrimSpace(line) == "esac" {
			break
		}
		parsed, terminated, err := scanShellLine(line)
		if err != nil {
			return nil, err
		}
		statements = append(statements, parsed...)
		if terminated {
			break
		}
	}
	return statements, nil
}

// permittedArmCommands are the only command words an arm may run. Both appear
// in the real still-running arm, and neither can change a shell variable, so a
// statement that begins with one assigns nothing and is skipped.
//
// The list is a WHITELIST on purpose. Enumerating the words that CAN set a
// variable -- export, declare, typeset, readonly, local, eval, source, read,
// getopts, mapfile, an `if`/`for`/`while` keyword, a shell function -- has no
// end, and growing that list one bypass at a time is #6194 exactly. Adding a
// third entry here is a deliberate act with a review behind it.
var permittedArmCommands = map[string]bool{"echo": true, "exit": true}

// effectiveStateAssignment returns the value the arm actually leaves in
// `state`, whether it assigns it at all, and an error when the arm is built
// from statements outside the grammar below.
//
// # Why the last assignment
//
// Bash runs an arm top to bottom and the LAST assignment is the one the `gh
// api` call below the case block reads, so `state=error; state=success`
// publishes `success`. Asking only whether the arm CONTAINS `state=error`
// accepts exactly that -- a cancelled dependency satisfying the required merge
// status (#6189 review). Only the surviving assignment is a claim about what
// the publisher does.
//
// # The accepted statement grammar
//
//	arm        := { statement }
//	statement  := assignment { blank assignment }   -- sets every name, persistently
//	            | command                            -- sets nothing
//	command    := ( "echo" | "exit" ) { blank word }
//
// A statement of nothing but assignments is bash's own rule for a persistent
// assignment, so `description=x state=success` sets both.
//
// # What it refuses, and why refusing beats guessing
//
// Every other statement shape returns errUnparseableShell, which callers turn
// into a loud validation error. The rule this replaced -- read the leading run
// of assignment words, stop at the first word that is not one -- read three
// ordinary spellings as assigning nothing at all (#6218 review round 3):
//
//	export state=failure            `export` ended the run before anything was seen
//	if true; then state=failure; fi `then` ended it, the same way
//	state=$'failure'                read as the literal `$failure`; now refused
//	                                by the scanner in requiredworkflow_shell.go
//
// Each one is bash publishing `failure` for gates that have merely not
// finished, which is the collapse #6075 removed. A MIXED statement is refused
// too, and that one errs the other way: in `state=error echo hi` the
// assignment lives only for the duration of `echo`, so reading it as the arm's
// verdict would credit the arm with a value bash does not leave behind.
//
// An `exit` earlier in the arm does not stop the walk, so assignments below
// one are read as if they ran. That can only report an arm bash would leave
// alone; it can never miss one bash does set.
func effectiveStateAssignment(statements []shellStatement) (string, bool, error) {
	value, found := "", false
	for _, statement := range statements {
		if len(statement) == 0 {
			continue
		}
		if _, _, ok := shellAssignment(statement[0]); !ok {
			if permittedArmCommands[statement[0].text] {
				continue
			}
			return "", false, fmt.Errorf(
				"%w: statement beginning %q is neither a run of assignments nor one of the commands "+
					"this publisher runs (echo, exit)", errUnparseableShell, statement[0].text)
		}
		for _, word := range statement {
			name, assigned, ok := shellAssignment(word)
			if !ok {
				return "", false, fmt.Errorf(
					"%w: %q follows an assignment in the same statement, which makes the assignment last "+
						"only for that command", errUnparseableShell, word.text)
			}
			if name == "state" {
				value, found = assigned, true
			}
		}
	}
	return value, found, nil
}

// armStateAssignment locates the AGGREGATE_CODE arm for one exit code and
// reports what that arm leaves in `state`. found is false when no such arm
// exists; err names the arm when it cannot be parsed, so a locator failure and
// a grammar failure reach the two validators below in the same shape.
func armStateAssignment(run string, code int) (state string, assigned, found bool, err error) {
	statements, found, err := aggregateCodeArm(run, code)
	if err != nil || !found {
		return "", false, found, err
	}
	state, assigned, err = effectiveStateAssignment(statements)
	if err != nil {
		return "", false, true, fmt.Errorf("%d) arm: %w", code, err)
	}
	return state, assigned, true, nil
}
