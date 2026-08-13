// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"strings"
	"testing"
	"time"
)

// Publish-safety half of the doc-lockstep guard (see doc_lockstep_test.go for
// the conventions). Two claims about what reaches a caller had almost no
// coverage until a hostile review measured them.
//
// README.md enumerates six things scopeSafe rejects and only the absolute-path
// one was tested. Deleting the `..` rejection passed the whole suite while
// Evaluate published repo_path=../../../etc/passwd into the Claude
// additionalContext string.
//
// Which Claude tools fire the hook at all was pinned by nothing.
// triggerFromClaudeTool decides it, and remapping "bash" from "edit" to
// "search" turned a Bash PreToolUse into a 324-character advisory with the
// suite green -- against a contract doc that says in as many words the hook
// must not run for shell commands.
//
// Both assert through Evaluate rather than against the helper directly,
// because publishing is what the claims are about: the scope ID lands in the
// CLI's `scope:` text line and inside additionalContext, and a helper-level
// assertion would not notice if the decision stopped depending on it.

// narrowRepoScope is a scope ID every rejection case below is compared
// against: same request, one unsafe field.
const narrowRepoScope = "services/api/handler.go"

// advisableInput is a request that advises, so a case that skips is skipping
// for the reason under test and not for an unrelated one.
func advisableInput() Input {
	return Input{
		Host:     supportedHostClaude,
		Enabled:  true,
		Trigger:  "read",
		RepoPath: narrowRepoScope,
		Budget:   DefaultBudget,
	}
}

// scopeRejection is one of the six kinds README.md's "Gotchas / invariants"
// section says scopeSafe refuses.
type scopeRejection struct {
	Kind string
	ID   string
	// AloneRejects records whether this ID is refused by its own named clause
	// only. Two of the six are not: `~` and `\` are outside the
	// [A-Za-z0-9._/:-] character class as well, so deleting either explicit
	// check changes no decision Evaluate can make. That is worth stating
	// rather than implying, because a test cannot go red on a mutation that
	// does not move production behavior -- and a reader who assumed it could
	// would trust this table for more than it proves.
	AloneRejects bool
}

// documentedScopeRejections transcribes README.md's list, one case per kind:
// "an absolute path, a `~` prefix, a URL, `..`, a backslash, or a character
// outside [A-Za-z0-9._/:-]".
func documentedScopeRejections() []scopeRejection {
	return []scopeRejection{
		{Kind: "absolute_path", ID: "/etc/passwd", AloneRejects: true},
		{Kind: "home_prefix", ID: "~/private/service.go"},
		{Kind: "url", ID: "https://internal.example.com/secret", AloneRejects: true},
		{Kind: "parent_traversal", ID: "../../../etc/passwd", AloneRejects: true},
		{Kind: "backslash", ID: `C:\Users\example\service.go`},
		{Kind: "outside_character_class", ID: "services/api handler$.go", AloneRejects: true},
	}
}

// TestDocLockstepScopeSafeRejectionsStayUnpublished drives every documented
// rejection kind through Evaluate and asserts the whole publish surface stays
// empty: the decision skips with reasonBroadScope, Output carries no Scope or
// PlannedCall, and the Claude additionalContext string is empty rather than
// carrying the ID.
func TestDocLockstepScopeSafeRejectionsStayUnpublished(t *testing.T) {
	t.Parallel()

	cases := documentedScopeRejections()
	if len(cases) != 6 {
		t.Fatalf("scope rejection cases = %d, want 6 -- one per kind README.md's invariants section names", len(cases))
	}
	alone := 0
	for _, tc := range cases {
		if tc.AloneRejects {
			alone++
		}
	}
	if alone != 4 {
		t.Fatalf("%d of %d rejection kinds are held by their own clause, want 4; the `~` and `\\` cases are also outside the character class, so deleting either explicit check moves no decision and this table cannot go red on it", alone, len(cases))
	}

	checked := 0
	for _, tc := range cases {
		input := advisableInput()
		input.RepoPath = tc.ID
		out := Evaluate(input)

		if out.Decision != decisionSkip || out.Reason != reasonBroadScope {
			t.Errorf("%s (%q): decision=%q reason=%q, want skip/%s; README.md lists this as a scopeSafe rejection", tc.Kind, tc.ID, out.Decision, out.Reason, reasonBroadScope)
		}
		if out.Scope != nil {
			t.Errorf("%s (%q): Output.Scope = %+v, want nil; the CLI echoes Scope.ID in its scope: line", tc.Kind, tc.ID, out.Scope)
		}
		if out.PlannedCall != nil {
			t.Errorf("%s (%q): Output.PlannedCall = %+v, want nil", tc.Kind, tc.ID, out.PlannedCall)
		}
		context := ClaudePreToolUseOutputForPreflight(out).HookSpecificOutput.AdditionalContext
		if context != "" {
			t.Errorf("%s (%q): additionalContext = %q, want empty; this string is what Claude receives", tc.Kind, tc.ID, context)
		}
		checked++
	}
	if checked != len(cases) {
		t.Fatalf("evaluated %d of %d rejection kinds", checked, len(cases))
	}

	// The paired positive. Without it every assertion above would also hold
	// for a scopeSafe that refused everything.
	advised := Evaluate(advisableInput())
	if advised.Decision != DecisionAdvise || advised.Scope == nil {
		t.Fatalf("the safe control request came back %q with scope %+v; the rejection assertions above would be vacuous", advised.Decision, advised.Scope)
	}
	if !strings.Contains(ClaudePreToolUseOutputForPreflight(advised).HookSpecificOutput.AdditionalContext, narrowRepoScope) {
		t.Fatal("the safe control request published no scope ID into additionalContext; the empty-context assertions above would be vacuous")
	}
}

// claudeToolTriggerClasses maps each Claude tool name this package translates
// to the trigger class it must produce. The rejected half is the contract
// doc's own exclusion sentence, spelled out one tool at a time: "The hook must
// not run for write, edit, format, delete, commit, push, shell, ... commands."
// The tools with no case of their own fall through triggerFromClaudeTool's
// default to their own lowercased name, which triggerAllowed refuses -- so
// they are here to keep that default from being changed into something
// permissive.
func claudeToolTriggerClasses() (advises, rejects map[string]string) {
	advises = map[string]string{
		"Read":        "read",
		"Grep":        "search",
		"Glob":        "glob",
		"LS":          "glob",
		"find_symbol": "symbol",
	}
	rejects = map[string]string{
		"Write":     "edit",
		"Edit":      "edit",
		"MultiEdit": "edit",
		"Bash":      "edit",
		"Format":    "format",
		"Delete":    "delete",
		"Commit":    "commit",
		"Push":      "push",
		"Shell":     "shell",
	}
	return advises, rejects
}

// TestDocLockstepClaudeToolTriggerClasses pins which Claude tools can produce
// hook output. AGENTS.md names triggerFromClaudeTool as the other half of
// "add a new trigger class" and nothing held it, so the mapping could be
// edited on its own: every trigger-class assertion in this package reads
// Input.Trigger, and the tool that produced it was never in the picture.
//
// It runs through MergeClaudePreToolUseInput and Evaluate so the assertion is
// about what a Claude PreToolUse payload actually gets back, not about a
// helper's return value.
func TestDocLockstepClaudeToolTriggerClasses(t *testing.T) {
	t.Parallel()

	advises, rejects := claudeToolTriggerClasses()
	if len(advises) != 5 || len(rejects) != 9 {
		t.Fatalf("mapping covers %d advising and %d rejected tools, want 5 and 9 (the seven command families the contract doc's exclusion sentence names, with Bash for shell, plus MultiEdit)", len(advises), len(rejects))
	}

	checked := 0
	for tool, wantClass := range rejects {
		input := Input{Host: supportedHostClaude, Enabled: true, RepoPath: narrowRepoScope, Budget: DefaultBudget}
		MergeClaudePreToolUseInput(&input, ClaudePreToolUseInput{HookEventName: "PreToolUse", ToolName: tool})
		out := Evaluate(input)

		if input.Trigger != wantClass {
			t.Errorf("tool %q maps to trigger %q, want %q", tool, input.Trigger, wantClass)
		}
		if triggerAllowed(wantClass) {
			t.Errorf("trigger class %q (from tool %q) is allowed; the contract doc says the hook must not run for this command family", wantClass, tool)
		}
		if out.Decision != decisionSkip || out.Reason != reasonDisallowedTrigger {
			t.Errorf("tool %q: decision=%q reason=%q, want skip/%s", tool, out.Decision, out.Reason, reasonDisallowedTrigger)
		}
		if context := ClaudePreToolUseOutputForPreflight(out).HookSpecificOutput.AdditionalContext; context != "" {
			t.Errorf("tool %q: additionalContext = %q, want empty; a write/shell tool must get no hook output", tool, context)
		}
		checked++
	}

	for tool, wantClass := range advises {
		input := Input{Host: supportedHostClaude, Enabled: true, RepoPath: narrowRepoScope, Budget: DefaultBudget}
		MergeClaudePreToolUseInput(&input, ClaudePreToolUseInput{HookEventName: "PreToolUse", ToolName: tool})
		out := Evaluate(input)

		if input.Trigger != wantClass {
			t.Errorf("tool %q maps to trigger %q, want %q", tool, input.Trigger, wantClass)
		}
		if out.Decision != DecisionAdvise {
			t.Errorf("tool %q: decision=%q, want advise; this is a read-family tool the hook is for", tool, out.Decision)
		}
		checked++
	}
	if checked != len(advises)+len(rejects) {
		t.Fatalf("evaluated %d of %d tools", checked, len(advises)+len(rejects))
	}

	// An explicit --trigger flag wins over the payload, so the mapping above
	// only decides the inferred case. Pinning that here keeps the two halves
	// of MergeClaudePreToolUseInput's contract in one place.
	explicit := Input{Host: supportedHostClaude, Enabled: true, Trigger: "read", RepoPath: narrowRepoScope, Budget: DefaultBudget}
	MergeClaudePreToolUseInput(&explicit, ClaudePreToolUseInput{HookEventName: "PreToolUse", ToolName: "Bash"})
	if explicit.Trigger != "read" {
		t.Errorf("an explicit trigger became %q after merging a Bash payload, want it preserved", explicit.Trigger)
	}
	if Evaluate(explicit).Decision != DecisionAdvise {
		t.Error("an explicit --trigger read did not advise; README.md says the flag wins over the inferred value")
	}
}

// TestDocLockstepClaudeToolExclusionSentenceIsIntact pins the contract-doc
// sentence claudeToolTriggerClasses transcribes, naming each excluded command
// family, so the doc and the mapping cannot drift apart in either direction.
func TestDocLockstepClaudeToolExclusionSentenceIsIntact(t *testing.T) {
	t.Parallel()

	sentences := []string{
		"The hook must not run for write, edit, format, delete, commit, push, shell,",
		"secret-management, provider, or deployment commands.",
	}
	if len(sentences) != 2 {
		t.Fatalf("pinned sentences = %d, want both lines of the exclusion rule", len(sentences))
	}
	for _, sentence := range sentences {
		missing, err := docsMissingLiteral(contractDocDir, []string{contractDocName}, sentence)
		if err != nil {
			t.Fatalf("read %s: %v", contractDocName, err)
		}
		for _, name := range missing {
			t.Errorf("%s no longer says %q; claudeToolTriggerClasses transcribes that sentence, so change both or neither", name, sentence)
		}
	}

	// Guard against the budget check masking a trigger finding: every
	// rejected tool above is evaluated with a budget in hand, not an expired
	// one.
	if DefaultBudget <= 0 {
		t.Fatalf("DefaultBudget = %v; the rejected-tool cases would skip on timeout instead of trigger", DefaultBudget)
	}
	out := Evaluate(Input{
		Host: supportedHostClaude, Enabled: true, Trigger: "read",
		RepoPath: narrowRepoScope, Budget: DefaultBudget,
		Elapsed: DefaultBudget + time.Millisecond,
	})
	if out.Reason != reasonTimeout {
		t.Fatalf("an expired budget gave reason %q, want %q; the masking check above assumes this ordering", out.Reason, reasonTimeout)
	}
}
