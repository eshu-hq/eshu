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

// excludedToolFamilyVerbs is the contract doc's exclusion sentence read the way
// it is written -- as command families, not as tool names. The mapping tests
// above enumerate names, and a host names its tools whatever it likes:
// NotebookEdit is in none of those lists, and a `case "notebookedit"` returning
// a read class produced a 324-character advisory for a notebook write with the
// whole suite green. Enumerating one more name would have left the next one.
func excludedToolFamilyVerbs() []string {
	return []string{"write", "edit", "format", "delete", "commit", "push", "shell", "secret", "provider", "deployment"}
}

// decoratedToolNames spells verb the ways a host actually names a tool in that
// family: alone, cased, prefixed, suffixed, and separated. These are shapes a
// real tool name takes, not an exhaustive set -- the point is that none of them
// may reach a read class, whichever one a host picks.
func decoratedToolNames(verb string) []string {
	capitalized := strings.ToUpper(verb[:1]) + verb[1:]
	names := []string{verb, capitalized, strings.ToUpper(verb), " " + capitalized + " "}
	for _, prefix := range []string{"Notebook", "Multi", "Web", "Bulk"} {
		names = append(names, prefix+capitalized)
	}
	for _, suffix := range []string{"File", "Tool", "Notebook"} {
		names = append(names, capitalized+suffix)
	}
	return append(names, verb+"_file", "pre_"+verb, verb+"-file", "pre-"+verb)
}

// TestDocLockstepExcludedToolFamiliesNeverReachAReadClass pins the exclusion
// sentence as the family rule it states. Every name built from an excluded verb
// must come back a skip with no published context, whatever the host called the
// tool.
func TestDocLockstepExcludedToolFamiliesNeverReachAReadClass(t *testing.T) {
	t.Parallel()

	// The control first: this request advises when its tool is a read-family
	// one, so a skip below is the family rule and not an unrelated ineligible
	// field.
	control := Input{Host: supportedHostClaude, Enabled: true, RepoPath: narrowRepoScope, Budget: DefaultBudget}
	MergeClaudePreToolUseInput(&control, ClaudePreToolUseInput{HookEventName: "PreToolUse", ToolName: "Read"})
	if Evaluate(control).Decision != DecisionAdvise {
		t.Fatal("the Read control did not advise; every assertion below would hold for a request that never advises")
	}

	verbs := excludedToolFamilyVerbs()
	if len(verbs) != 10 {
		t.Fatalf("excluded family verbs = %d, want the 10 the contract doc's exclusion sentence names", len(verbs))
	}

	checked := 0
	for _, verb := range verbs {
		for _, tool := range decoratedToolNames(verb) {
			input := Input{Host: supportedHostClaude, Enabled: true, RepoPath: narrowRepoScope, Budget: DefaultBudget}
			MergeClaudePreToolUseInput(&input, ClaudePreToolUseInput{HookEventName: "PreToolUse", ToolName: tool})
			out := Evaluate(input)

			if triggerAllowed(input.Trigger) {
				t.Errorf("tool %q maps to trigger class %q, which triggerAllowed accepts; the contract doc says the hook must not run for %s commands", tool, input.Trigger, verb)
			}
			if out.Decision != decisionSkip || out.Reason != reasonDisallowedTrigger {
				t.Errorf("tool %q: decision=%q reason=%q, want skip/%s", tool, out.Decision, out.Reason, reasonDisallowedTrigger)
			}
			if context := ClaudePreToolUseOutputForPreflight(out).HookSpecificOutput.AdditionalContext; context != "" {
				t.Errorf("tool %q: additionalContext = %q, want empty; a %s-family tool must get no hook output", tool, context, verb)
			}
			checked++
		}
	}
	if checked != len(verbs)*len(decoratedToolNames("edit")) {
		t.Fatalf("evaluated %d tool names, want %d", checked, len(verbs)*len(decoratedToolNames("edit")))
	}
}

// toolClassTranslation reports the class tool maps to and whether that is a
// translation rather than the documented default. triggerFromClaudeTool's
// default hands back the tool's own lowercased name, so a name equal to a
// trigger class advises without anything having translated it; a translation is
// the function deciding that one name means a different class.
func toolClassTranslation(tool string) (class string, translated bool) {
	class = triggerFromClaudeTool(tool)
	return class, class != strings.ToLower(strings.TrimSpace(tool))
}

// TestDocLockstepClaudeToolTranslationsAreEnumerated pins claudeToolTriggerClasses
// as a complete list of the translations triggerFromClaudeTool performs, not a
// sample of them.
//
// It compares two sets. One is every string literal the production files
// declare, run through the mapping and kept when the mapping translated it --
// a new `case` has to name its tool, and that name lands in a literal. The
// other is the pinned table, filtered the same way. A new translation into a
// read class fails on the first set carrying a name the second does not; a
// pinned row whose case was deleted fails the other way round.
//
// This is the reason the check is not a source scan: triggerFromClaudeTool
// returns strings from an expression default, so the closed-switch scanner
// cannot read it, and teaching a scanner one more shape is what the four
// earlier generations of the trigger guard each did.
func TestDocLockstepClaudeToolTranslationsAreEnumerated(t *testing.T) {
	t.Parallel()

	literals, err := sourceStringLiterals(".")
	if err != nil {
		t.Fatalf("read the package's string literals: %v", err)
	}
	if len(literals) == 0 {
		t.Fatal("found no string literals in the production files; this comparison would be vacuous")
	}

	fromSource := map[string]string{}
	for _, literal := range literals {
		if class, translated := toolClassTranslation(literal); translated {
			fromSource[strings.ToLower(strings.TrimSpace(literal))] = class
		}
	}

	advises, rejects := claudeToolTriggerClasses()
	fromTable := map[string]string{}
	for _, tools := range []map[string]string{advises, rejects} {
		for tool := range tools {
			if class, translated := toolClassTranslation(tool); translated {
				fromTable[strings.ToLower(strings.TrimSpace(tool))] = class
			}
		}
	}
	if len(fromTable) == 0 {
		t.Fatal("no pinned tool name translates to a different class; the comparison would be vacuous")
	}

	for tool, class := range fromSource {
		want, pinned := fromTable[tool]
		switch {
		case !pinned:
			t.Errorf("the production files name %q, which triggerFromClaudeTool translates to class %q, and claudeToolTriggerClasses does not list it; a tool the docs never named is deciding whether the hook runs", tool, class)
		case want != class:
			t.Errorf("tool %q translates to class %q, pinned as %q", tool, class, want)
		}
	}
	for tool, class := range fromTable {
		if _, found := fromSource[tool]; !found {
			t.Errorf("claudeToolTriggerClasses pins %q as translating to %q, but no production string literal carries that name; restore the case or drop the row", tool, class)
		}
	}
}
