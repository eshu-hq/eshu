// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"strings"
	"testing"
)

// Content half of the doc-lockstep guard (see doc_lockstep_test.go for the
// conventions the whole guard follows). Its siblings pin which requests get an
// advisory; this file pins what the advisory says.
//
// Each claim here was measured unpinned before it was written: mutating it left
// the whole suite green while the emitted additionalContext string, or the
// recommended MCP tool, changed under a caller's feet.

// TestDocLockstepTruthLabelsOnTheWire pins the two Truth labels doc.go and
// README.md describe as fixed for every result: level "advisory" and profile
// "local_preflight".
//
// The level was already held from the sibling package -- go/cmd/eshu's
// assistant_hook_preflight_test.go asserts "truth.level=advisory" inside the
// hook JSON -- but the profile was held by nothing. Changing it to
// "remote_production" passed everything while telling Claude the advisory came
// from a deployed backend. Both are asserted here, at the constant and in the
// string a host actually reads, so the pin does not depend on which package a
// future split leaves them in.
func TestDocLockstepTruthLabelsOnTheWire(t *testing.T) {
	t.Parallel()

	out := Evaluate(advisableInput())
	if out.Decision != DecisionAdvise {
		t.Fatalf("control request decided %q, want advise; the label assertions below would be vacuous", out.Decision)
	}
	if out.Truth.Level != "advisory" {
		t.Errorf("Truth.Level = %q, want %q; every Evaluate result is advisory, and a host reading a stronger label would treat preflight output as canonical graph truth", out.Truth.Level, "advisory")
	}
	if out.Truth.Profile != "local_preflight" {
		t.Errorf("Truth.Profile = %q, want %q; this package resolves scope from flags and a decoded payload, so nothing it emits came from a deployed backend", out.Truth.Profile, "local_preflight")
	}
	if out.Truth.Truncated {
		t.Error("Truth.Truncated = true, want false; Evaluate returns one scope and one planned call, so there is nothing for it to truncate")
	}

	context := ClaudePreToolUseOutputForPreflight(out).HookSpecificOutput.AdditionalContext
	for _, want := range []string{"truth.level=advisory", "truth.profile=local_preflight", "truncated=false"} {
		if !strings.Contains(context, want) {
			t.Errorf("additionalContext does not carry %q; that string is what the host reads, so a label the Output carries but the string does not is not published", want)
		}
	}
}

// adviseDisclaimer is the sentence every advisory ends with. It is the only
// thing in the published string telling a host how much weight to give the
// advice, and deleting it shortened additionalContext by 73 characters with
// nothing red.
const adviseDisclaimer = "This is advisory context only; do not treat it as canonical graph truth."

// TestDocLockstepAdvisoryCarriesTheDisclaimer pins that sentence, and pins that
// a skip publishes nothing at all -- the disclaimer matters because the string
// exists, so the assertion has to prove the string is otherwise non-empty.
func TestDocLockstepAdvisoryCarriesTheDisclaimer(t *testing.T) {
	t.Parallel()

	advised := ClaudePreToolUseOutputForPreflight(Evaluate(advisableInput())).HookSpecificOutput.AdditionalContext
	if advised == "" {
		t.Fatal("the advise control published an empty additionalContext; the disclaimer assertion below would be vacuous")
	}
	if !strings.Contains(advised, adviseDisclaimer) {
		t.Errorf("additionalContext does not end with %q; without it the host has nothing telling it this is not canonical graph truth.\ngot: %s", adviseDisclaimer, advised)
	}

	denied := advisableInput()
	denied.Permission = permissionDenied
	if skipped := ClaudePreToolUseOutputForPreflight(Evaluate(denied)).HookSpecificOutput.AdditionalContext; skipped != "" {
		t.Errorf("a skip published additionalContext %q, want empty", skipped)
	}
}

// TestDocLockstepPlannedCallToolPerScopeKind pins which bounded MCP tool
// Evaluate recommends for each scope kind, and the argument key it hands that
// tool. AGENTS.md says a scope kind and its plannedCallForScope case must change
// together, because a kind falling through to the default
// get_code_relationship_story is silently wrong -- but only the repo_path and
// service branches were covered, and then only incidentally by
// preflight_test.go. Repointing workload and environment at get_service_story
// passed the whole suite while every deployment-tracing request got the wrong
// tool.
func TestDocLockstepPlannedCallToolPerScopeKind(t *testing.T) {
	t.Parallel()

	const id = "checkout"
	cases := []struct {
		kind    string
		input   func(in Input) Input
		wantSVC string
		wantArg string
	}{
		{kind: "repo_path", input: func(in Input) Input { in.RepoPath = "services/api/handler.go"; return in }, wantSVC: "get_code_relationship_story", wantArg: "repo_path"},
		{kind: "entity_id", input: func(in Input) Input { in.EntityID = id; return in }, wantSVC: "get_code_relationship_story", wantArg: "entity_id"},
		{kind: "service", input: func(in Input) Input { in.Service = id; return in }, wantSVC: "get_service_story", wantArg: "service"},
		{kind: "workload", input: func(in Input) Input { in.Workload = id; return in }, wantSVC: "trace_deployment_chain", wantArg: "workload"},
		{kind: "environment", input: func(in Input) Input { in.Environment = id; return in }, wantSVC: "trace_deployment_chain", wantArg: "environment"},
		{kind: "resource", input: func(in Input) Input { in.Resource = id; return in }, wantSVC: "investigate_resource", wantArg: "resource_handle"},
	}
	if len(cases) != 6 {
		t.Fatalf("scope-kind cases = %d, want one per candidate scopeFromInput offers", len(cases))
	}

	for _, tc := range cases {
		base := Input{Host: supportedHostClaude, Enabled: true, Trigger: "read", Budget: DefaultBudget}
		out := Evaluate(tc.input(base))
		if out.Decision != DecisionAdvise {
			t.Errorf("%s: decision = %q, want advise", tc.kind, out.Decision)
			continue
		}
		if out.Scope == nil || out.Scope.Kind != tc.kind {
			t.Errorf("%s: Output.Scope = %+v, want kind %q", tc.kind, out.Scope, tc.kind)
			continue
		}
		if out.PlannedCall == nil {
			t.Errorf("%s: advise Output carries no PlannedCall", tc.kind)
			continue
		}
		if out.PlannedCall.Tool != tc.wantSVC {
			t.Errorf("%s: recommended tool = %q, want %q; a scope kind answered by the wrong MCP tool sends the assistant somewhere that cannot answer it", tc.kind, out.PlannedCall.Tool, tc.wantSVC)
		}
		if _, ok := out.PlannedCall.Arguments[tc.wantArg]; !ok {
			t.Errorf("%s: PlannedCall.Arguments = %v, want a %q key; the tool cannot resolve a scope it was handed under another name", tc.kind, out.PlannedCall.Arguments, tc.wantArg)
		}
	}
}

// TestDocLockstepRepoRelativePathRefusesEscapes pins repoRelativePath's own
// contract: an absolute tool path is published only when it resolves inside the
// payload's cwd.
//
// This one is asserted at the function rather than through Evaluate on purpose,
// and the reason is worth writing down. Dropping the `strings.HasPrefix(rel,
// "..")` guard changes the merged Input.RepoPath from "" to "../etc/passwd",
// but every Evaluate decision holds: scopeSafe refuses "..", so the request
// still skips with reasonBroadScope and publishes nothing. Measured on a full
// disk copy, 2026-08-13 -- the two probes agree, and only the merged RepoPath
// moves. So this is a second independent check on the same escape, in the shape
// of the `~` and `\` clauses README.md already flags: real, and unable to move
// a decision on its own. A test through Evaluate could not go red on it, which
// is exactly why this one is not written that way.
func TestDocLockstepRepoRelativePathRefusesEscapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cwd  string
		path string
		want string
	}{
		{name: "inside_cwd_becomes_relative", cwd: "/repo", path: "/repo/services/api/handler.go", want: "services/api/handler.go"},
		{name: "already_relative_is_cleaned", cwd: "/repo", path: "./services/api/handler.go", want: "services/api/handler.go"},
		{name: "outside_cwd_is_refused", cwd: "/repo", path: "/etc/passwd", want: ""},
		{name: "sibling_of_cwd_is_refused", cwd: "/repo/services", path: "/repo/secrets/key.pem", want: ""},
		{name: "absolute_without_cwd_is_refused", cwd: "", path: "/etc/passwd", want: ""},
		{name: "cwd_itself_is_not_a_path", cwd: "/repo", path: "/repo", want: ""},
		{name: "blank_is_refused", cwd: "/repo", path: "   ", want: ""},
	}
	if len(cases) != 7 {
		t.Fatalf("path cases = %d, want one per branch repoRelativePath takes", len(cases))
	}

	published := 0
	for _, tc := range cases {
		got := repoRelativePath(tc.cwd, tc.path)
		if got != tc.want {
			t.Errorf("%s: repoRelativePath(%q, %q) = %q, want %q", tc.name, tc.cwd, tc.path, got, tc.want)
		}
		if got != "" {
			published++
		}
	}
	if published != 2 {
		t.Errorf("%d of %d cases produced a repo path, want 2; if none does, the refusals above prove only that the function returns empty for everything", published, len(cases))
	}
}
