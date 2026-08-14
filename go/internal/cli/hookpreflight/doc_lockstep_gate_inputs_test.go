// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"strings"
	"testing"
)

// Acceptance-gate half of the doc-lockstep guard (see doc_lockstep_test.go for
// the conventions the whole guard follows).
//
// Five generations of hostile review taught this package one thing about the
// trigger gate: a closed set pinned by a list of examples is not pinned. The
// list only ever holds values someone already thought of, so a widening that
// admits a value nobody listed passes every assertion.
//
// Evaluate gates acceptance on more than the trigger, and the same question was
// never asked of the rest. It has two other closed sets, and both were held by
// enumeration when this file was written:
//
//	Input.Host      -- accepting "cursor" as a second host made a second host
//	                   advise, with Output.Host="cursor" on the wire, against a
//	                   contract doc that forbids claiming host support without
//	                   implementation proof. Suite green.
//	the scope-ID character class -- adding '$' to scopeSafe's allowed switch
//	                   published repo_path=services/api/$SECRET.go into the
//	                   Claude additionalContext string. Suite green, because the
//	                   one character-class case in documentedScopeRejections
//	                   also carries a space, which still rejected it.
//
// So each is pinned here as the property it enforces rather than as a list of
// values that happen to fail today: Evaluate advises for exactly one host, and
// for exactly the ID characters README.md documents.

// eligibleExceptHost is a request that advises whenever its host is accepted.
// Every other ineligible condition is ruled out, so the decision turns on the
// host alone.
func eligibleExceptHost() Input {
	return Input{
		Enabled:  true,
		Trigger:  "read",
		RepoPath: narrowRepoScope,
		Budget:   DefaultBudget,
	}
}

// evaluateAdvisesHost reports whether Evaluate advises for host on an otherwise
// eligible request, through the real Evaluate rather than a reading of its
// switch.
func evaluateAdvisesHost(host string) bool {
	input := eligibleExceptHost()
	input.Host = host
	return Evaluate(input).Decision == DecisionAdvise
}

// hostAccepted is what the host gate is supposed to do, written from
// supportedHostClaude's own comment: exactly one host, after normalizeInput
// folds case and trims. Written out rather than calling the production
// predicate, because there is no production predicate -- the gate is a
// comparison inside Evaluate's switch, which is the thing under test.
func hostAccepted(host string) bool {
	return strings.ToLower(strings.TrimSpace(host)) == supportedHostClaude
}

// hostSweepMaxLen bounds the exhaustive arm of the host sweep. It is shorter
// than the trigger sweep's because the neighbourhood arm carries the reach
// here: the host names that matter are the ones the contract doc's table
// already names, and a second accepted host is overwhelmingly likely to be one
// of them or a spelling of one.
const hostSweepMaxLen = 3

// docNamedHosts are the host families the contract doc's "Supported Host
// Boundary" table names, plus spellings normalizeInput is documented to fold.
// Every one of them except "claude" must skip.
func docNamedHosts() []string {
	return []string{"claude", "codex", "cursor", "CLAUDE", " Claude ", "claude-code", "claudecode", "other"}
}

// TestDocLockstepEvaluateAdvisesForExactlyOneHost pins the supported-host set
// the way the trigger classes are pinned: as an equality over a swept input,
// not as a list of hosts that are refused today.
func TestDocLockstepEvaluateAdvisesForExactlyOneHost(t *testing.T) {
	t.Parallel()

	// Controls first. Without them the comparisons below would also hold for a
	// request that never advises, or one that always does.
	if !evaluateAdvisesHost(supportedHostClaude) {
		t.Fatal("Evaluate does not advise for the supported host on an otherwise eligible request; the sweep below would compare two constants")
	}
	if evaluateAdvisesHost("codex") {
		t.Fatal(`Evaluate advises for host "codex", which the contract doc lists as guidance-only`)
	}

	compared, advised, mismatched := 0, 0, 0
	var reported []string
	check := func(host string) {
		compared++
		got := evaluateAdvisesHost(host)
		if got {
			advised++
		}
		if got == hostAccepted(host) {
			return
		}
		mismatched++
		if len(reported) < 8 {
			reported = append(reported, host)
		}
	}

	// Arm one: every string over the trigger sweep's alphabet up to
	// hostSweepMaxLen. Lowercase and whitespace-free for the same reason the
	// trigger sweep is -- normalizeInput folds those, so a mismatch there would
	// be normalizeInput doing its documented job.
	var sweep func(prefix []byte)
	sweep = func(prefix []byte) {
		check(string(prefix))
		if len(prefix) == hostSweepMaxLen {
			return
		}
		for i := 0; i < len(triggerSweepAlphabet); i++ {
			sweep(append(prefix, triggerSweepAlphabet[i]))
		}
	}
	sweep(make([]byte, 0, hostSweepMaxLen))
	sweptExhaustively := compared

	// Arm two: the hosts the contract doc names, at their real length, plus the
	// case and whitespace spellings of the accepted one.
	named := docNamedHosts()
	for _, host := range named {
		check(host)
	}

	// Arm three: every string one character edit away from those, which is
	// where a second accepted host lands when it is a variant of a name the doc
	// already carries.
	neighbours := oneEditNeighbourhood(named)
	for _, host := range neighbours {
		check(host)
	}

	// Arm four: every string literal the production files declare, so a host
	// accepted by name somewhere in the source is compared whether or not
	// anyone listed it here.
	literals, err := sourceStringLiterals(".")
	if err != nil {
		t.Fatalf("read the package's string literals: %v", err)
	}
	if len(literals) == 0 {
		t.Fatal("found no string literals in the production files; that arm would be vacuous")
	}
	for _, literal := range literals {
		check(literal)
	}

	for _, host := range reported {
		t.Errorf("host %q: Evaluate advises=%v, want %v; %s is the only host this package claims, and the contract doc's Implementation Gate forbids claiming support for another without implementation proof for it",
			host, !hostAccepted(host), hostAccepted(host), supportedHostClaude)
	}
	if mismatched > len(reported) {
		t.Errorf("%d hosts disagreed in total; the first %d are named above", mismatched, len(reported))
	}

	// The counts are part of the assertion: a sweep that stopped early, or one
	// where nothing advised, passes every comparison above.
	if want := expectedHostSweepSize(); sweptExhaustively != want {
		t.Errorf("exhaustive arm compared %d hosts, want %d; the sweep did not cover every string over %q up to length %d", sweptExhaustively, want, triggerSweepAlphabet, hostSweepMaxLen)
	}
	if advised == 0 {
		t.Errorf("no host out of %d advised; the comparison held only because Evaluate never advises", compared)
	}
	t.Logf("compared %d hosts (%d exhaustive, %d doc-named, %d one-edit neighbours, %d source literals); %d advised", compared, sweptExhaustively, len(named), len(neighbours), len(literals), advised)
}

// expectedHostSweepSize is the number of strings over triggerSweepAlphabet of
// length 0 through hostSweepMaxLen, computed independently of the walk that
// generates them so a bug in the walk cannot define its own expectation.
func expectedHostSweepSize() int {
	total, power := 0, 1
	for length := 0; length <= hostSweepMaxLen; length++ {
		total += power
		power *= len(triggerSweepAlphabet)
	}
	return total
}

// TestDocLockstepSupportedHostBoundaryDoc pins the contract-doc rows the host
// gate implements, so the code and the doc cannot drift apart in either
// direction.
func TestDocLockstepSupportedHostBoundaryDoc(t *testing.T) {
	t.Parallel()

	rows := []string{
		"| Claude Code-style PreToolUse | Supported for opt-in local preflight planning",
		"| Codex | Guidance-only until",
		"| Cursor | Guidance-only until",
		"| Other MCP clients | Guidance-only unless",
		"support for a target host unless a PR adds implementation proof for that host:",
	}
	for _, row := range rows {
		missing, err := docsMissingLiteral(contractDocDir, []string{contractDocName}, row)
		if err != nil {
			t.Fatalf("read %s: %v", contractDocName, err)
		}
		for _, name := range missing {
			t.Errorf("%s no longer says %q; the host gate implements that table, so change both or neither", name, row)
		}
	}

	// One supported row and three guidance-only ones. Counting is what makes a
	// new row visible: adding a fourth family as supported leaves every literal
	// above intact.
	guidance, err := docLiteralOccurrences(contractDocDir, contractDocName, "| Guidance-only")
	if err != nil {
		t.Fatalf("read %s: %v", contractDocName, err)
	}
	if guidance != 3 {
		t.Errorf("%s has %d guidance-only host rows, want 3 (Codex, Cursor, other MCP clients); a host family moved between tiers", contractDocName, guidance)
	}
	supported, err := docLiteralOccurrences(contractDocDir, contractDocName, "| Supported for")
	if err != nil {
		t.Fatalf("read %s: %v", contractDocName, err)
	}
	if supported != 1 {
		t.Errorf("%s has %d supported host rows, want 1; this package advises for %q alone", contractDocName, supported, supportedHostClaude)
	}
}

// documentedScopeIDRune is README.md's "a character outside [A-Za-z0-9._/:-]"
// rejection, written out from that sentence rather than read back off
// scopeSafe, so a character added to the production switch disagrees with it.
func documentedScopeIDRune(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '.', '_', '-', '/', ':':
		return true
	}
	return false
}

// expectedScopeIDRuneCount is how many of the runes swept below README.md's
// character class admits: 26 lowercase, 26 uppercase, 10 digits, and the five
// punctuation characters. Computed from the sentence rather than from the loop,
// so a sweep that quietly stopped early cannot define its own expectation.
const expectedScopeIDRuneCount = 26 + 26 + 10 + 5

// TestDocLockstepScopeIDCharacterClassIsExact pins which characters may reach
// Output.Scope.ID, and with it the Claude additionalContext string the CLI
// publishes.
//
// documentedScopeRejections covers the six rejection kinds README.md names, one
// input each, and that is the wrong shape for this clause: its
// character-class input is "services/api handler$.go", which carries a space as
// well as a '$'. Adding '$' to scopeSafe's allowed switch leaves the space
// rejecting it, so the whole table stays green while a scope ID carrying a
// shell variable name is published. One input per kind cannot pin a class of
// characters -- only a sweep over the class can.
func TestDocLockstepScopeIDCharacterClassIsExact(t *testing.T) {
	t.Parallel()

	// The runes swept: every ASCII code point, plus a few beyond it. The
	// non-ASCII ones are here because the character-class loop ranges over
	// runes, so a widening written as a range comparison rather than a case
	// list would show up there first.
	runes := make([]rune, 0, 132)
	for r := rune(0); r < 128; r++ {
		runes = append(runes, r)
	}
	runes = append(runes, 'é', '中', ' ', '\U0001f600')

	accepted, mismatched := 0, 0
	var reported []rune
	for _, r := range runes {
		id := "svc" + string(r) + "api"
		input := advisableInput()
		input.RepoPath = id
		out := Evaluate(input)
		advises := out.Decision == DecisionAdvise
		if advises {
			accepted++
			context := ClaudePreToolUseOutputForPreflight(out).HookSpecificOutput.AdditionalContext
			if !strings.Contains(context, id) {
				t.Errorf("scope ID %q advised but does not appear in additionalContext; this sweep is about what gets published, so a scope the Output carries and the string does not would make it prove nothing", id)
			}
		}
		if advises == documentedScopeIDRune(r) {
			continue
		}
		mismatched++
		if len(reported) < 8 {
			reported = append(reported, r)
		}
	}

	for _, r := range reported {
		t.Errorf("scope ID character %q (U+%04X): Evaluate advises=%v, README.md's [A-Za-z0-9._/:-] class says %v; a character class change decides what reaches the published additionalContext string",
			string(r), r, !documentedScopeIDRune(r), documentedScopeIDRune(r))
	}
	if mismatched > len(reported) {
		t.Errorf("%d characters disagreed in total; the first %d are named above", mismatched, len(reported))
	}
	if accepted != expectedScopeIDRuneCount {
		t.Errorf("%d of the swept characters produced an advisory, want %d; README.md's class is 26 letters twice, 10 digits, and . _ - / :", accepted, expectedScopeIDRuneCount)
	}
}
