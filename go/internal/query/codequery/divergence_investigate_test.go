// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestCodeHandlerDivergenceInvestigateReturnsBoundedNextSteps pins the
// investigate leg: one finding by (kind, fingerprint) with the same member
// shape as the findings page, plus next_steps an MCP client can execute
// without guessing — call-chain and file-range calls per member with
// arguments filled in, bounded and truncated when the group is large.
func TestCodeHandlerDivergenceInvestigateReturnsBoundedNextSteps(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: fakeDivergenceStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/investigate",
		bytes.NewBufferString(`{"repo_id":"repo-x","kind":"exact","fingerprint":"fp-fixture"}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope struct {
		Data struct {
			Finding struct {
				FindingID string `json:"finding_id"`
				Members   []struct {
					EntityID string `json:"entity_id"`
					Package  string `json:"package"`
				} `json:"members"`
			} `json:"finding"`
			NextSteps []struct {
				Tool    string         `json:"tool"`
				Args    map[string]any `json:"args"`
				Purpose string         `json:"purpose"`
			} `json:"next_steps"`
			Truncated bool `json:"truncated"`
		} `json:"data"`
		Truth struct {
			Level string `json:"level"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, w.Body.String())
	}
	if envelope.Data.Finding.FindingID == "" {
		t.Fatal("finding_id must be set")
	}
	if got, want := len(envelope.Data.Finding.Members), 2; got != want {
		t.Fatalf("members = %d, want %d", got, want)
	}
	if envelope.Data.Finding.Members[0].Package == "" {
		t.Fatal("members must carry their owning package")
	}
	seenCallChain, seenFileLines := false, false
	for _, step := range envelope.Data.NextSteps {
		if step.Tool == "" || step.Purpose == "" {
			t.Fatalf("next step must name a tool and purpose, got %+v", step)
		}
		switch step.Tool {
		case "find_function_call_chain":
			seenCallChain = true
			if step.Args["start_entity_id"] == nil || step.Args["repo_id"] == nil {
				t.Fatalf("call-chain step must fill start_entity_id and repo_id, got %v", step.Args)
			}
		case "get_file_lines":
			seenFileLines = true
			for _, key := range []string{"repo_id", "relative_path", "start_line", "end_line"} {
				if step.Args[key] == nil {
					t.Fatalf("file-lines step must fill %s, got %v", key, step.Args)
				}
			}
		default:
			t.Fatalf("unexpected next-step tool %q", step.Tool)
		}
	}
	if !seenCallChain || !seenFileLines {
		t.Fatal("next steps must include call-chain and file-range calls")
	}
	if envelope.Truth.Level != "derived" {
		t.Fatalf("truth.level = %q, want derived", envelope.Truth.Level)
	}
}

// TestCodeHandlerDivergenceInvestigateIncludeTestsOptsBackIn pins the P1
// leg from the #6877 owner review: a test-file-only group 404s by default
// but assembles when the caller opts test files back in, matching the
// findings list behavior with include_tests=true.
func TestCodeHandlerDivergenceInvestigateIncludeTestsOptsBackIn(t *testing.T) {
	t.Parallel()

	post := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		handler := &CodeHandler{Content: fakeDivergenceStore{}, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/code/divergence/investigate", bytes.NewBufferString(body))
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}

	excluded := post(t, `{"repo_id":"repo-x","kind":"exact","fingerprint":"fp-tests"}`)
	if got, want := excluded.Code, http.StatusNotFound; got != want {
		t.Fatalf("default status = %d, want %d body=%s", got, want, excluded.Body.String())
	}

	included := post(t, `{"repo_id":"repo-x","kind":"exact","fingerprint":"fp-tests","include_tests":true}`)
	if got, want := included.Code, http.StatusOK; got != want {
		t.Fatalf("include_tests status = %d, want %d body=%s", got, want, included.Body.String())
	}
	var envelope struct {
		Data struct {
			Finding struct {
				Members []struct {
					EntityID string `json:"entity_id"`
				} `json:"members"`
			} `json:"finding"`
			Suppressions map[string]int `json:"suppressions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(included.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, included.Body.String())
	}
	if got, want := len(envelope.Data.Finding.Members), 2; got != want {
		t.Fatalf("opted-in members = %d, want %d", got, want)
	}
	if got := envelope.Data.Suppressions["test_file"]; got != 0 {
		t.Fatalf("opted-in test_file suppressions = %d, want 0", got)
	}
}

// TestCodeHandlerDivergenceInvestigateAcceptsQualifiedKind pins the P1
// leg from the owner review: a kind copied verbatim from a findings
// entry (parallel_implementation.exact) addresses the same family as
// the short form instead of 400ing.
func TestCodeHandlerDivergenceInvestigateAcceptsQualifiedKind(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"parallel_implementation.exact", "parallel_implementation.renamed"} {
		handler := &CodeHandler{Content: fakeDivergenceStore{}, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v0/code/divergence/investigate",
			bytes.NewBufferString(`{"repo_id":"repo-x","kind":"`+kind+`","fingerprint":"fp-fixture"}`),
		)
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		// fp-fixture members exist in the fake for either family read,
		// so both qualified spellings must validate and assemble.
		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("kind %q status = %d, want %d body=%s", kind, got, want, w.Body.String())
		}
	}
	if err := (DivergenceInvestigateRequest{RepoID: "r", Kind: "drifted"}).validate(); err == nil {
		t.Fatal("validate() must still reject unknown kinds")
	}
}

// TestCodeHandlerDivergenceInvestigateUnknownFingerprint404s pins the miss
// leg: an unknown fingerprint is a 404, not an empty finding.
func TestCodeHandlerDivergenceInvestigateUnknownFingerprint404s(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: fakeDivergenceStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/divergence/investigate",
		bytes.NewBufferString(`{"repo_id":"repo-x","kind":"exact","fingerprint":"fp-missing"}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}
