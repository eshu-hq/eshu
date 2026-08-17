// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoManifestPath locates the committed demo-first-answers manifest from the
// package directory, so these tests read the real acceptance oracle rather
// than a fixture that could drift from it.
func repoManifestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "..", "specs", "demo-first-answers.v1.yaml")
}

func TestLoadDemoManifest_ReadsTheCommittedOracle(t *testing.T) {
	t.Parallel()
	m, err := LoadManifest(repoManifestPath(t))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.Questions) < 5 {
		t.Fatalf("questions = %d, want at least the five demo questions", len(m.Questions))
	}
	q := m.Questions[0]
	if q.ID == "" || strings.TrimSpace(q.Question) == "" {
		t.Errorf("first question is missing id or text: %+v", q)
	}
	// The manifest exists so the demo executes a declared surface instead of
	// inventing a query route. If this ever parses empty, the demo would have
	// nothing real to call.
	if q.Execute.Kind == "" || q.Execute.Ref == "" {
		t.Fatalf("first question declares no execute surface: %+v", q.Execute)
	}
	if len(q.ExpectedAnswer.RequiredResponseFields) == 0 {
		t.Errorf("first question declares no required_response_fields; the answer could not be checked")
	}
}

func TestLoadDemoManifest_FirstQuestionIsTheMCPServiceStory(t *testing.T) {
	t.Parallel()
	m, err := LoadManifest(repoManifestPath(t))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	q := m.Questions[0]
	if q.Execute.Kind != executeMCP {
		t.Errorf("first question execute kind = %q, want %q", q.Execute.Kind, executeMCP)
	}
	if q.Execute.Ref != "get_service_story" {
		t.Errorf("first question execute ref = %q, want get_service_story", q.Execute.Ref)
	}
	if got := q.Execute.Arguments["workload_id"]; got != "workload:api-svc" {
		t.Errorf("workload_id = %v, want workload:api-svc", got)
	}
}

func TestAskDemoQuestion_MCPPostsToolsCallAndReturnsTheAnswer(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte( // The real MCP envelope: content[] is human text, structuredContent
			// is the typed payload the manifest's required fields live in.
			`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"api-svc runs in workload:api-svc"}],
			"structuredContent":{
			"answer_metadata":{"truth":{"level":"exact"}},
			"answer_packet":{"summary":"api-svc runs in workload:api-svc"},
			"api_surface":{"routes":[]}}}}`))
	}))
	defer srv.Close()

	q := Question{
		ID:       "q1",
		Question: "which workload?",
		Execute: Execute{
			Kind:      executeMCP,
			Ref:       "get_service_story",
			Arguments: map[string]any{"workload_id": "workload:api-svc"},
		},
	}
	q.ExpectedAnswer.RequiredResponseFields = []string{"answer_metadata", "answer_packet", "api_surface"}

	ans, err := ExecuteQuestion(context.Background(), srv.URL, srv.URL, "k", q)
	if err != nil {
		t.Fatalf("ExecuteQuestion: %v", err)
	}
	if !strings.Contains(gotPath, "mcp") {
		t.Errorf("posted to %q, want the MCP endpoint", gotPath)
	}
	if gotBody["method"] != "tools/call" {
		t.Errorf("method = %v, want tools/call", gotBody["method"])
	}
	params, _ := gotBody["params"].(map[string]any)
	if params["name"] != "get_service_story" {
		t.Errorf("params.name = %v, want get_service_story", params["name"])
	}
	args, _ := params["arguments"].(map[string]any)
	if args["workload_id"] != "workload:api-svc" {
		t.Errorf("arguments.workload_id = %v, want workload:api-svc", args["workload_id"])
	}
	if ans.Answer == "" {
		t.Error("answer is empty")
	}
	if len(ans.Truth) == 0 {
		t.Error("answer carries no truth labels")
	}
}

func TestAskDemoQuestion_MissingRequiredFieldIsAnError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// answer_packet is absent: the tool replied, but not with the shape
		// the manifest says this question's answer must have.
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"structuredContent":{"answer_metadata":{},"api_surface":{}}}}`))
	}))
	defer srv.Close()

	q := Question{ID: "q1", Question: "q", Execute: Execute{Kind: executeMCP, Ref: "get_service_story"}}
	q.ExpectedAnswer.RequiredResponseFields = []string{"answer_metadata", "answer_packet", "api_surface"}

	_, err := ExecuteQuestion(context.Background(), srv.URL, srv.URL, "k", q)
	if err == nil {
		t.Fatal("error = nil; a reply missing a required field must not pass as an answer")
	}
	if !strings.Contains(err.Error(), "answer_packet") {
		t.Errorf("error %q does not name the missing field", err)
	}
}

func TestAskDemoQuestion_HTTPExecutesTheDeclaredRoute(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"incident":{"id":"PSCD1"},"services":[{"name":"api-svc"}],"truth":{"level":"exact"}}`))
	}))
	defer srv.Close()

	q := Question{ID: "q3", Question: "which services?", Execute: Execute{
		Kind: executeHTTP, Ref: "GET /api/v0/incidents/PSCD1/context",
	}}
	q.ExpectedAnswer.RequiredResponseFields = []string{"incident", "services"}

	if _, err := ExecuteQuestion(context.Background(), srv.URL, srv.URL, "k", q); err != nil {
		t.Fatalf("ExecuteQuestion: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v0/incidents/PSCD1/context" {
		t.Errorf("path = %q, want the manifest's declared route", gotPath)
	}
}

func TestAskDemoQuestion_UnknownExecuteKindFailsLoudly(t *testing.T) {
	t.Parallel()
	q := Question{ID: "qX", Execute: Execute{Kind: "carrier-pigeon", Ref: "x"}}
	_, err := ExecuteQuestion(context.Background(), "http://127.0.0.1:1", "http://127.0.0.1:1", "k", q)
	if err == nil {
		t.Fatal("error = nil; an unrecognized execute kind must fail rather than be skipped")
	}
}

func TestDemoUp_MintsAnEphemeralKeyAndPassesItToCompose(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{}
	rt := newTestDemoRuntime(exec)

	if _, err := rt.Up(context.Background()); err != nil {
		t.Fatalf("up: %v", err)
	}
	// The demo runtime overlay refuses to start mcp-server with no resolvable
	// credential source (#5168), so "zero credentials" has to mean the command
	// mints one rather than that the stack runs without one.
	if rt.apiKey == "" {
		t.Fatal("no ephemeral demo key was minted; mcp-server will refuse to start")
	}
	if len(rt.apiKey) < 24 {
		t.Errorf("ephemeral key is %d chars; too short to be a credential", len(rt.apiKey))
	}
	var sawKeyEnv bool
	for _, e := range exec.envs {
		for _, kv := range e {
			if strings.HasPrefix(kv, "ESHU_DEMO_API_KEY=") && strings.TrimPrefix(kv, "ESHU_DEMO_API_KEY=") == rt.apiKey {
				sawKeyEnv = true
			}
		}
	}
	if !sawKeyEnv {
		t.Errorf("ESHU_DEMO_API_KEY was never passed to compose; envs=%v", exec.envs)
	}
}

func TestDemoEphemeralKeys_AreNotReusedAcrossRuns(t *testing.T) {
	t.Parallel()
	a, err := newEphemeralKey()
	if err != nil {
		t.Fatalf("newEphemeralKey: %v", err)
	}
	b, err := newEphemeralKey()
	if err != nil {
		t.Fatalf("newEphemeralKey: %v", err)
	}
	if a == b {
		t.Error("two demo runs minted the same key; it must be per-run, not a constant")
	}
}

func TestDemoStatus_RecoversTheKeyFromTheRunningStack(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{replies: map[string]demoExecReply{
		// A running project, and the stack hands back its own key.
		"ps --quiet":            {out: []byte("abc123\n")},
		"printenv ESHU_API_KEY": {out: []byte("demo-recovered-key\n")},
	}}
	rt := newTestDemoRuntime(exec)
	rt.apiKey = "" // status runs in a fresh process; up's ephemeral key is gone.

	var probedWith string
	rt.probe = func(_ context.Context, _, key string) (IndexStatus, error) {
		probedWith = key
		return IndexStatus{Status: "healthy", RepositoryCount: 6}, nil
	}

	res, err := rt.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	// Without recovery the probe goes out unauthenticated, gets 401, and a
	// healthy stack is reported as not ready — indistinguishable from a real
	// failure.
	if probedWith != "demo-recovered-key" {
		t.Errorf("probed with key %q, want the key recovered from the running stack", probedWith)
	}
	if !res.Ready {
		t.Error("healthy stack with an empty queue reported as not ready")
	}
}

// lookup builds the environment-lookup function the wrapper passes in as
// os.Getenv. A map keeps these tests parallel-safe and independent of whatever
// the developer happens to have exported in their shell.
func lookup(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

func TestDemoBases_FollowTheOverlayPortOverrides(t *testing.T) {
	t.Parallel()
	// The overlay binds ${ESHU_DEMO_API_PORT:-18080} and
	// ${ESHU_DEMO_MCP_PORT:-18091}. A second demo that moves its ports to
	// avoid the first one's must still be probed where it actually listens.
	getenv := lookup(map[string]string{
		EnvAPIPort:  "19080",
		EnvMCPPort:  "19091",
		EnvBindAddr: "127.0.0.1",
	})
	if got := APIBase(getenv); got != "http://127.0.0.1:19080" {
		t.Errorf("APIBase() = %q, want the overridden API port", got)
	}
	if got := MCPBase(getenv); got != "http://127.0.0.1:19091" {
		t.Errorf("MCPBase() = %q, want the overridden MCP port", got)
	}
}

func TestDemoBases_DefaultToTheOverlayDefaults(t *testing.T) {
	t.Parallel()
	getenv := lookup(nil)
	if got := APIBase(getenv); got != "http://127.0.0.1:18080" {
		t.Errorf("APIBase() = %q, want the overlay default", got)
	}
	if got := MCPBase(getenv); got != "http://127.0.0.1:18091" {
		t.Errorf("MCPBase() = %q, want the overlay default", got)
	}
}

// TestResolveComposeFile_HonoursTheOverride proves EnvComposeFile short-circuits
// the parent-directory walk. The wrapper passes os.Getenv in, so an operator
// pointing at their own overlay must not be sent walking up from the working
// directory instead.
func TestResolveComposeFile_HonoursTheOverride(t *testing.T) {
	t.Parallel()
	const override = "/somewhere/else/my-overlay.yaml"
	got, err := ResolveComposeFile(t.TempDir(), lookup(map[string]string{EnvComposeFile: override}))
	if err != nil {
		t.Fatalf("ResolveComposeFile: %v", err)
	}
	if got != override {
		t.Errorf("resolved %q, want the override %q", got, override)
	}
}

func TestResolveDemoComposeFile_FindsItFromASubdirectory(t *testing.T) {
	t.Parallel()
	// An installed binary is rarely invoked from the repo root, and a bare
	// relative path makes Compose fail to find the overlay.
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ComposeFileName)
	if err := os.WriteFile(want, []byte("name: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveComposeFile(nested, lookup(nil))
	if err != nil {
		t.Fatalf("ResolveComposeFile: %v", err)
	}
	if got != want {
		t.Errorf("resolved %q, want %q", got, want)
	}
}

func TestResolveDemoComposeFile_SaysWhatItLookedForWhenAbsent(t *testing.T) {
	t.Parallel()
	_, err := ResolveComposeFile(t.TempDir(), lookup(nil))
	if err == nil {
		t.Fatal("error = nil; a missing overlay must fail with guidance, not a bare compose error")
	}
	if !strings.Contains(err.Error(), ComposeFileName) {
		t.Errorf("error %q does not name the file it searched for", err)
	}
}

func TestDemoDown_RefusesAProjectItDoesNotOwn(t *testing.T) {
	t.Parallel()
	// Compose labels each container with the config files that created it.
	// A project built from someone else's compose file is not ours to remove.
	exec := &fakeDemoExec{replies: map[string]demoExecReply{
		"config_files": {out: []byte("/home/someone/their-stack/docker-compose.yaml\n")},
	}}
	rt := newTestDemoRuntime(exec)

	err := rt.Down(context.Background())
	if err == nil {
		t.Fatal("down() error = nil; it must refuse a project it did not create")
	}
	if !strings.Contains(err.Error(), "did not start") {
		t.Errorf("error %q does not explain the refusal", err)
	}
	for _, c := range exec.calls {
		if strings.Contains(strings.Join(c, " "), "down") {
			t.Errorf("down ran against a project it does not own; calls=%v", exec.calls)
		}
	}
}

func TestDemoDown_ProceedsOnItsOwnProject(t *testing.T) {
	t.Parallel()
	exec := &fakeDemoExec{replies: map[string]demoExecReply{
		"config_files": {out: []byte("/repo/" + ComposeFileName + "\n")},
	}}
	rt := newTestDemoRuntime(exec)

	if err := rt.Down(context.Background()); err != nil {
		t.Fatalf("down: %v", err)
	}
	if !exec.sawContaining("--remove-orphans") {
		t.Errorf("down did not run; calls=%v", exec.calls)
	}
}

// TestRunnableFormSurvivesShellQuoting runs the printed command through a real
// shell and checks the arguments JSON comes back byte-identical.
//
// The assertion is deliberately not "the string contains an escape sequence":
// a hand-written escaping check passes whenever the code and the test share the
// same wrong idea of POSIX quoting. Handing the line to /bin/sh is the only
// version that fails when the quoting is actually broken, which is what an
// operator pasting the line would hit.
func TestRunnableFormSurvivesShellQuoting(t *testing.T) {
	args := map[string]any{"repository": "it's-a-repo", "note": `he said "hi"`}
	q := Question{Execute: Execute{Kind: executeMCP, Ref: "get_context", Arguments: args}}

	line := q.RunnableForm()
	_, quoted, found := strings.Cut(line, "--arguments ")
	if !found {
		t.Fatalf("RunnableForm() = %q, want an --arguments segment", line)
	}

	// printf %s re-emits exactly what the shell parsed as the single argument.
	out, err := exec.Command("/bin/sh", "-c", "printf %s "+quoted).CombinedOutput()
	if err != nil {
		t.Fatalf("shell rejected %s: %v (output %q)", quoted, err, out)
	}

	want, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(want) {
		t.Errorf("shell parsed arguments as %q, want %q", out, want)
	}
}

// TestLoadDemoManifest_ResolvesTheFlatSurfaceShape pins the fallback branch
// that resolves a question declaring its callable directly on surface, rather
// than nested under surface.execute.
//
// Both shapes are present in the committed manifest, and an earlier draft
// resolved the flat one to zero values -- which does not fail loading, it just
// leaves the question uncallable at the moment the demo tries to answer it.
// The arguments assertion matters for the same reason: a call that reaches the
// right tool with no arguments comes back with the wrong answer, not an error.
func TestLoadDemoManifest_ResolvesTheFlatSurfaceShape(t *testing.T) {
	t.Parallel()
	m, err := LoadManifest(repoManifestPath(t))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	var flat *Question
	for i := range m.Questions {
		if m.Questions[i].Surface.Execute.Kind == "" && m.Questions[i].Surface.Kind == executeMCP {
			flat = &m.Questions[i]
			break
		}
	}
	if flat == nil {
		t.Fatal("no flat-shape question in the manifest; this test no longer guards the fallback branch")
	}
	if flat.Execute.Kind != executeMCP {
		t.Errorf("%s Execute.Kind = %q, want %q", flat.ID, flat.Execute.Kind, executeMCP)
	}
	if flat.Execute.Ref != flat.Surface.Ref || flat.Execute.Ref == "" {
		t.Errorf("%s Execute.Ref = %q, want the surface ref %q", flat.ID, flat.Execute.Ref, flat.Surface.Ref)
	}
	if len(flat.Execute.Arguments) != len(flat.Surface.Arguments) {
		t.Errorf("%s Execute.Arguments = %v, want the surface arguments %v",
			flat.ID, flat.Execute.Arguments, flat.Surface.Arguments)
	}
}
