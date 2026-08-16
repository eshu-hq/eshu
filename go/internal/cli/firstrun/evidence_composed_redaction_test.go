// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

// Synthetic leak sentinels. These are not real credentials; they exist so a
// composed string that smuggles a raw endpoint into an operator artifact fails
// loudly instead of passing a field-scoped assertion one field over.
const (
	// leakSentinelAPI stands in for a password embedded in the API base URL.
	leakSentinelAPI = "LEAKSENTINEL-1"
	// leakSentinelMCP stands in for a password embedded in the MCP endpoint.
	leakSentinelMCP = "LEAKSENTINEL-2"
	// leakSentinelTarget stands in for a private path segment in the repo target.
	leakSentinelTarget = "LEAKSENTINEL-3"
	// leakSentinelQuery stands in for a credential passed as a URL query
	// parameter. The other three sentinels sit in userinfo or a path segment, so
	// no assertion here could vary the query-string axis and it stayed open.
	leakSentinelQuery = "LEAKSENTINEL-4"
)

// composedLeakFixture builds a failed first-run result whose credential-bearing
// values reach the evidence report only through composed strings: the verify
// hint that becomes the diagnosis cause, the MCP-endpoint-is-API summary, and
// the next-step command that embeds the repo target.
//
// It deliberately routes through the production composition functions
// (firstRunStartHint, firstRunVerifySignal, classifyOnboardingFailure,
// firstRunNextSteps) rather than hand-building an Diagnostic, so the
// test proves the real path and not a fixture that mimics it.
func composedLeakFixture(t *testing.T) (Result, string, string) {
	t.Helper()

	apiBase := "http://svcuser:" + leakSentinelAPI + "@127.0.0.1:59413"
	mcpEndpoint := "http://mcpuser:" + leakSentinelMCP + "@127.0.0.1:59413/api/v0?token=" + leakSentinelQuery
	repoTarget := "/home/" + leakSentinelTarget + "/work/repo"

	// The classifier resolves the MCP endpoint through the Deps seam. The
	// production wrapper wires it to the .env file under the app home, which
	// stays in go/cmd/eshu; its own test pins that config read, so this
	// fixture wires the seam to the raw endpoint and everything downstream --
	// the start hint, the classifier, the composed summary -- is the real
	// production path.
	deps := Deps{ResolveMCPEndpoint: func() string { return mcpEndpoint }}

	detection := firstRunRuntimeDetection{
		Shape:       ShapeDockerCompose,
		ComposeFile: "docker-compose.yaml",
		Detail:      "compose stack detected",
	}

	result := NewResult(apiBase)
	result.RuntimeShape = detection.Shape
	result.RepoTarget = repoTarget
	result.RepoIndexed = "failed"
	result.Readiness = "not ready"

	// Reproduce the real verify-failure path: the start hint becomes the step
	// detail, the step detail becomes the verify error, and the verify error is
	// preserved as the diagnosis root cause.
	hint := firstRunStartHint(detection, apiBase, false, "docker compose up -d")
	result = result.addStep("verify runtime", StepFailed, hint)
	verifyErr := fmt.Errorf("verify runtime: %s", hint)
	result = attachFirstRunDiagnostic(result, firstRunVerifySignal(deps, detection, apiBase, verifyErr))
	result.NextSteps = firstRunNextSteps(result, detection)
	result.Truth = Truth(result, "")

	if result.Diagnostic == nil {
		t.Fatal("fixture did not attach a diagnostic; the classifier path changed")
	}
	if result.Diagnostic.Class != ClassMCPEndpointIsAPI {
		t.Fatalf("diagnostic class = %q, want %q; the fixture no longer exercises the composed summary",
			result.Diagnostic.Class, ClassMCPEndpointIsAPI)
	}
	return result, mcpEndpoint, repoTarget
}

// assertNoSentinels fails with the offending surface named when any synthetic
// sentinel survives into a rendered artifact. It never echoes the full body, so
// a failure message cannot itself become a leak.
func assertNoSentinels(t *testing.T, surface, body string) {
	t.Helper()
	for _, sentinel := range []string{leakSentinelAPI, leakSentinelMCP, leakSentinelTarget, leakSentinelQuery} {
		if strings.Contains(body, sentinel) {
			t.Errorf("%s leaks %s through a composed string", surface, sentinel)
		}
	}
}

// TestRedactEndpointBoundaryCorpus drives redactEndpoint through the shared
// corpus in internal/urlredact. internal/reportbundle drives its free-text walk
// through the identical rows, in TestRedactFreeTextBoundaryCorpus.
//
// One table, two walks, on purpose. The fixture this replaces claimed to pin
// "both halves of the query rule" while every one of its five cases used a
// single "&", no ";", no nested "?" and no fragment — the axis it named was the
// one axis it never varied. So the two walks drifted on exactly those
// boundaries and nothing went red. A row either walk cannot handle carries its
// reason, and urlredact's own check fails when a reason stops being true, so a
// stale exemption cannot quietly widen what the corpus permits.
func TestRedactEndpointBoundaryCorpus(t *testing.T) {
	for _, tc := range urlredact.BoundaryCases() {
		t.Run(tc.Name, func(t *testing.T) {
			got := redactEndpoint(tc.Input)
			// The leak check runs FIRST, and fatally. Behind the exact-output
			// assertion it could never fire: a leaking row fails that first and
			// t.Fatalf ends the subtest, so the one message naming a shipped
			// credential was dead code. The diff is also the wrong message for
			// a leak — it prints the body, which is exactly what checkSecret
			// refuses to echo.
			if err := tc.CheckEndpointSecret(got); err != nil {
				t.Fatal(err)
			}
			// Kept, and second: it is what catches over-removal and a rewritten
			// escape, neither of which the leak check can see.
			if got != tc.WantEndpoint {
				t.Fatalf("redactEndpoint(%q)\n got %q\nwant %q", tc.Input, got, tc.WantEndpoint)
			}
			// The report can be re-rendered from a saved envelope, so the
			// redacted form has to be a fixed point.
			if again := redactEndpoint(got); again != got {
				t.Fatalf("redactEndpoint is not idempotent: second pass on %q gave %q", got, again)
			}
		})
	}
}

// TestRedactEndpointKeepsBenignParametersAlongsideARedactedOne pins the
// over-redaction side on a multi-parameter endpoint: only the credential-named
// parameter loses its value, and the ones around it keep their position, so an
// operator can still match the target against their own config.
func TestRedactEndpointKeepsBenignParametersAlongsideARedactedOne(t *testing.T) {
	const raw = "http://127.0.0.1:8080/x?repo=demo&token=" + leakSentinelQuery + "&page=2"
	const want = "http://127.0.0.1:8080/x?repo=demo&token=redacted&page=2"

	if got := redactEndpoint(raw); got != want {
		t.Fatalf("redactEndpoint(%q) = %q, want %q", raw, got, want)
	}
}

// TestScrubEvidenceTextDoesNotCorruptURLsForASeparatorOnlyTarget pins the other
// direction of the substitution gate. The gate asks whether a raw path is
// distinctive enough to identify, and "//" is not: it appears inside every
// absolute URL, so substituting it rewrote "https://h/z" to "https:.../h/z" in
// composed text. That is corrupting rather than leaking — the operator gets a
// mangled endpoint they cannot match against their own config, and stage two no
// longer recognizes it as a URL to redact either.
//
// A repo target with an empty final element has the same problem for a
// different reason: it redacts to ".../", which identifies nothing.
func TestScrubEvidenceTextDoesNotCorruptURLsForASeparatorOnlyTarget(t *testing.T) {
	const text = "could not reach https://h/z from the compose network"

	// Every one of these is a substring of the URL in text, so a stage-one
	// substitution that reaches inside a URL span rewrites it.
	for _, target := range []string{"//", "//h/z", "/h/z"} {
		t.Run(target, func(t *testing.T) {
			if got := scrubEvidenceText(text, []string{target}); got != text {
				t.Fatalf("scrubEvidenceText with target %q\n got %q\nwant %q", target, got, text)
			}
		})
	}
}

// TestScrubEvidenceTextStillSubstitutesRealTargets is the companion that keeps
// the gate above from being satisfied by refusing to substitute anything. The
// short-absolute-path case is the one a byte-length gate used to let through.
func TestScrubEvidenceTextStillSubstitutesRealTargets(t *testing.T) {
	tests := []struct {
		target string
		text   string
		want   string
	}{
		{target: "/u/bob", text: "eshu story /u/bob", want: "eshu story .../bob"},
		{
			target: "/home/" + leakSentinelTarget + "/work/repo",
			text:   "eshu story /home/" + leakSentinelTarget + "/work/repo",
			want:   "eshu story .../repo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := scrubEvidenceText(tt.text, []string{tt.target}); got != tt.want {
				t.Fatalf("scrubEvidenceText(%q, %q) = %q, want %q", tt.text, tt.target, got, tt.want)
			}
		})
	}
}

// TestEvidenceRedactsComposedStrings proves that credentials reaching the
// evidence report through composed strings — the diagnosis summary, the
// diagnosis cause, and the next-step commands — are redacted on every rendering
// surface, not merely in the endpoint fields a name-keyed redactor was aimed at.
//
// Regression guard: the existing redaction tests place a credential only in
// ServiceURL and MCPEndpoint on a result with no diagnostic and a credential-free
// repo target, so they never exercise the composed paths and pass while the
// artifact leaks.
func TestEvidenceRedactsComposedStrings(t *testing.T) {
	result, mcpEndpoint, _ := composedLeakFixture(t)
	report := BuildEvidence(result, &EvidenceInputs{MCPEndpoint: mcpEndpoint})

	md, err := renderEvidenceMarkdown(report)
	if err != nil {
		t.Fatalf("renderEvidenceMarkdown: %v", err)
	}
	assertNoSentinels(t, "markdown artifact", md)

	jsonBytes, err := renderEvidenceJSON(report)
	if err != nil {
		t.Fatalf("renderEvidenceJSON: %v", err)
	}
	assertNoSentinels(t, "json artifact", string(jsonBytes))

	var term strings.Builder
	RenderEvidenceTerminal(&term, report)
	assertNoSentinels(t, "terminal summary", term.String())
}

// TestEvidenceRedactsComposedNextCommandTarget proves the success path, where
// firstRunNextSteps composes "eshu story <repo target>" from the raw target that
// SelectedTarget redacts one field over. This is a separate fixture because the
// story next-step is emitted only when the run succeeded; the failure fixture
// returns static compose/local next-steps and never exercises it.
func TestEvidenceRedactsComposedNextCommandTarget(t *testing.T) {
	repoTarget := "/home/" + leakSentinelTarget + "/work/repo"

	result := successEvidenceResult()
	result.RepoTarget = repoTarget
	result.NextSteps = firstRunNextSteps(result, firstRunRuntimeDetection{Shape: ShapeExistingAPI})

	report := BuildEvidence(result, nil)
	if report.SelectedTarget == repoTarget {
		t.Fatal("SelectedTarget was not redacted; the fixture no longer isolates the composed path")
	}

	md, err := renderEvidenceMarkdown(report)
	if err != nil {
		t.Fatalf("renderEvidenceMarkdown: %v", err)
	}
	assertNoSentinels(t, "markdown next commands", md)

	jsonBytes, err := renderEvidenceJSON(report)
	if err != nil {
		t.Fatalf("renderEvidenceJSON: %v", err)
	}
	assertNoSentinels(t, "json next commands", string(jsonBytes))

	var term strings.Builder
	RenderEvidenceTerminal(&term, report)
	assertNoSentinels(t, "terminal next commands", term.String())
}

// TestEvidenceRedactsShortAbsoluteRepoTarget proves a short absolute path is
// substituted inside composed text, not just in the field that carries it.
//
// The substitution used to be gated on raw byte length. "/u/bob" is six bytes,
// so it stayed whole inside the composed next-step command while SelectedTarget
// one field over already showed ".../bob" — the two disagreed on the same
// artifact, and the username redactPath exists to hide was readable in one of
// them. The gate is structural now: absolute, with at least two separators.
func TestEvidenceRedactsShortAbsoluteRepoTarget(t *testing.T) {
	const shortTarget = "/u/bob"

	result := successEvidenceResult()
	result.RepoTarget = shortTarget
	result.NextSteps = firstRunNextSteps(result, firstRunRuntimeDetection{Shape: ShapeExistingAPI})

	report := BuildEvidence(result, nil)
	if report.SelectedTarget != ".../bob" {
		t.Fatalf("SelectedTarget = %q, want %q; the fixture no longer isolates the composed path",
			report.SelectedTarget, ".../bob")
	}

	for _, command := range report.NextCommands {
		if strings.Contains(command, shortTarget) {
			t.Errorf("next command %q keeps the raw absolute target while SelectedTarget shows %q",
				command, report.SelectedTarget)
		}
	}

	md, err := renderEvidenceMarkdown(report)
	if err != nil {
		t.Fatalf("renderEvidenceMarkdown: %v", err)
	}
	if strings.Contains(md, shortTarget) {
		t.Errorf("markdown artifact keeps the raw absolute target %q", shortTarget)
	}
}

// TestEvidenceScrubsNestedTruthValues proves the truth metadata is scrubbed at
// every depth. The scrub read only top-level strings, so a credential one level
// down, or inside an array, went into the artifact verbatim. The companion in
// go/cmd/eshu's first_run_evidence_cmd_test.go drives the same nesting through
// the real `first-run report` command, since that cobra path lives there.
func TestEvidenceScrubsNestedTruthValues(t *testing.T) {
	const (
		nestedSentinel = "LEAKSENTINEL-5"
		arraySentinel  = "LEAKSENTINEL-6"
		deepSentinel   = "LEAKSENTINEL-7"
	)
	endpoint := func(sentinel string) string {
		return "http://svcuser:" + sentinel + "@127.0.0.1:59413/x"
	}

	result := successEvidenceResult()
	result.Truth = map[string]any{
		"level":  "exact",
		"source": map[string]any{"endpoint": endpoint(nestedSentinel)},
		"notes":  []any{"answered from " + endpoint(arraySentinel)},
		"probes": []any{map[string]any{"target": endpoint(deepSentinel)}},
	}

	report := BuildEvidence(result, nil)
	sentinels := []string{nestedSentinel, arraySentinel, deepSentinel}
	for _, format := range []string{EvidenceFormatMarkdown, EvidenceFormatJSON} {
		data, err := RenderEvidenceArtifact(report, format)
		if err != nil {
			t.Fatalf("RenderEvidenceArtifact(%s): %v", format, err)
		}
		for _, sentinel := range sentinels {
			if strings.Contains(string(data), sentinel) {
				t.Errorf("evidence artifact (%s) leaks %s from nested truth metadata", format, sentinel)
			}
		}
	}
}

// TestEvidenceArtifactOnDiskRedactsComposedStrings proves the 0600 artifact an
// operator hands to support carries no credential in its composed strings.
func TestEvidenceArtifactOnDiskRedactsComposedStrings(t *testing.T) {
	result, mcpEndpoint, _ := composedLeakFixture(t)
	report := BuildEvidence(result, &EvidenceInputs{MCPEndpoint: mcpEndpoint})

	for _, format := range []string{EvidenceFormatMarkdown, EvidenceFormatJSON} {
		path := filepath.Join(t.TempDir(), "evidence."+format)
		if err := WriteEvidenceArtifact(report, format, path); err != nil {
			t.Fatalf("WriteEvidenceArtifact(%s): %v", format, err)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
		if err != nil {
			t.Fatalf("read artifact(%s): %v", format, err)
		}
		assertNoSentinels(t, "on-disk "+format+" artifact", string(data))
	}
}

// TestRedactEndpointDelegatesTheQueryWalkForTheDifferential is the bridge that
// lets internal/reportbundle's differential stand for the production endpoint
// walk.
//
// That differential compares redactFreeText against urlredact.Query, because
// package main is not importable from another package. This test closes the gap
// from the other side: over the identical rows, redactEndpoint must remove
// exactly the fragments Query removes. redactEndpoint adds URL parsing, the
// userinfo rule and the fragment rule around Query and touches no query
// boundary, so if that ever stops being true this goes red rather than the
// differential quietly measuring a walk the artifact does not use.
func TestRedactEndpointDelegatesTheQueryWalkForTheDifferential(t *testing.T) {
	cases := urlredact.DifferentialCases()
	if len(cases) == 0 {
		t.Fatal("the differential is empty, so this test would pass vacuously")
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			endpoint := redactEndpoint("http://127.0.0.1:8080/x?" + tc.Input)
			query := urlredact.Query(tc.Input, evidenceRedactedMarker)
			for _, fragment := range tc.Fragments() {
				if strings.Contains(endpoint, fragment) != strings.Contains(query, fragment) {
					t.Fatalf("redactEndpoint and urlredact.Query disagree on a fragment of %q", tc.Input)
				}
			}
		})
	}
}
