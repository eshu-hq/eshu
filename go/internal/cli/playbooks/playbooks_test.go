// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbooks

import (
	"errors"
	"strings"
	"testing"
)

// fakeClient records the one call the Run functions make and returns a canned
// envelope by mutating result through the same any-typed seam the real client
// uses, so the decode path under test is the production one.
type fakeClient struct {
	gotPath string
	gotBody any
	fill    func(result any)
	err     error
}

func (f *fakeClient) GetEnvelope(path string, result any) error {
	f.gotPath = path
	if f.err != nil {
		return f.err
	}
	if f.fill != nil {
		f.fill(result)
	}
	return nil
}

func (f *fakeClient) PostEnvelope(path string, body, result any) error {
	f.gotPath = path
	f.gotBody = body
	if f.err != nil {
		return f.err
	}
	if f.fill != nil {
		f.fill(result)
	}
	return nil
}

func TestParseInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     []string
		want    map[string]string
		wantErr string
	}{
		{name: "empty", raw: nil, want: map[string]string{}},
		{
			name: "multiple pairs trimmed",
			raw:  []string{" service_name = payments-api ", "environment=prod"},
			want: map[string]string{"service_name": "payments-api", "environment": "prod"},
		},
		{name: "empty value allowed", raw: []string{"environment="}, want: map[string]string{"environment": ""}},
		{name: "missing separator", raw: []string{"environment"}, wantErr: "input must use key=value form"},
		{name: "empty key", raw: []string{" =prod"}, wantErr: "input must use key=value form"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseInputs(tt.raw)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("ParseInputs(%v) error = %v, want %q", tt.raw, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseInputs(%v): %v", tt.raw, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseInputs(%v) = %v, want %v", tt.raw, got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("ParseInputs(%v)[%q] = %q, want %q", tt.raw, k, got[k], v)
				}
			}
		})
	}
}

func TestRunListWritesIndentedEnvelope(t *testing.T) {
	t.Parallel()

	client := &fakeClient{fill: func(result any) {
		envelope, ok := result.(*ListEnvelope)
		if !ok {
			t.Fatalf("result type = %T, want *ListEnvelope", result)
		}
		envelope.Data = map[string]any{"playbooks": []any{"<service_story_citation>"}}
		envelope.Truth = map[string]any{"level": "exact"}
	}}
	var out strings.Builder
	if err := RunList(&out, client); err != nil {
		t.Fatalf("RunList: %v", err)
	}
	if got, want := client.gotPath, "/api/v0/query-playbooks"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	want := "{\n  \"data\": {\n    \"playbooks\": [\n      \"<service_story_citation>\"\n    ]\n  },\n  \"truth\": {\n    \"level\": \"exact\"\n  },\n  \"error\": null\n}\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestRunResolvePostsTrimmedPlaybookID(t *testing.T) {
	t.Parallel()

	client := &fakeClient{fill: func(result any) {
		envelope, ok := result.(*ResolveEnvelope)
		if !ok {
			t.Fatalf("result type = %T, want *ResolveEnvelope", result)
		}
		envelope.Data.Resolved.PlaybookID = "service_story_citation"
	}}
	var out strings.Builder
	err := RunResolve(&out, client, ResolveOptions{
		PlaybookID: "  service_story_citation  ",
		Inputs:     map[string]string{"environment": "prod"},
	})
	if err != nil {
		t.Fatalf("RunResolve: %v", err)
	}
	if got, want := client.gotPath, "/api/v0/query-playbooks/resolve"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	body, ok := client.gotBody.(map[string]any)
	if !ok {
		t.Fatalf("body type = %T, want map[string]any", client.gotBody)
	}
	if got, want := body["playbook_id"], "service_story_citation"; got != want {
		t.Fatalf("playbook_id = %v, want %v", got, want)
	}
	inputs, ok := body["inputs"].(map[string]string)
	if !ok || inputs["environment"] != "prod" {
		t.Fatalf("inputs = %v, want environment=prod", body["inputs"])
	}
	if !strings.Contains(out.String(), `"playbook_id": "service_story_citation"`) {
		t.Fatalf("output = %q, want resolved playbook_id", out.String())
	}
}

// The headline envelope invariant: a capability or query failure arrives as a
// non-nil envelope error member, prints in-band as part of the JSON document,
// and the command still exits 0 (nil error). The package docs warn against
// "fixing" this into an exit code; this test pins it so the warning has teeth.
func TestRunListPrintsInBandEnvelopeError(t *testing.T) {
	t.Parallel()

	client := &fakeClient{fill: func(result any) {
		envelope, ok := result.(*ListEnvelope)
		if !ok {
			t.Fatalf("result type = %T, want *ListEnvelope", result)
		}
		envelope.Error = &EnvelopeError{Code: "not_found", Message: "no such playbook"}
	}}
	var out strings.Builder
	if err := RunList(&out, client); err != nil {
		t.Fatalf("in-band envelope error must not fail the command: %v", err)
	}
	if !strings.Contains(out.String(), `"message": "no such playbook"`) {
		t.Fatalf("output = %q, want the envelope error in-band", out.String())
	}
}

// Same invariant for resolve: the envelope's error member is payload, not an
// exit code.
func TestRunResolvePrintsInBandEnvelopeError(t *testing.T) {
	t.Parallel()

	client := &fakeClient{fill: func(result any) {
		envelope, ok := result.(*ResolveEnvelope)
		if !ok {
			t.Fatalf("result type = %T, want *ResolveEnvelope", result)
		}
		envelope.Error = &EnvelopeError{Code: "capability_unavailable", Message: "resolver offline"}
	}}
	var out strings.Builder
	if err := RunResolve(&out, client, ResolveOptions{PlaybookID: "service_story_citation"}); err != nil {
		t.Fatalf("in-band envelope error must not fail the command: %v", err)
	}
	if !strings.Contains(out.String(), `"message": "resolver offline"`) {
		t.Fatalf("output = %q, want the envelope error in-band", out.String())
	}
}

// fetchResolve's doc comment promises the request contract is testable without
// an output writer; this is that test. Path, trimmed ID, and body shape are
// pinned against the raw fetch seam directly.
func TestFetchResolveRequestContractWithoutWriter(t *testing.T) {
	t.Parallel()

	client := &fakeClient{fill: func(result any) {
		envelope, ok := result.(*ResolveEnvelope)
		if !ok {
			t.Fatalf("result type = %T, want *ResolveEnvelope", result)
		}
		envelope.Data.Resolved.PlaybookID = "service_story_citation"
	}}
	envelope, err := fetchResolve(client, ResolveOptions{
		PlaybookID: "\tservice_story_citation \n",
		Inputs:     map[string]string{"environment": "prod"},
	})
	if err != nil {
		t.Fatalf("fetchResolve: %v", err)
	}
	if got, want := client.gotPath, "/api/v0/query-playbooks/resolve"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	body, ok := client.gotBody.(map[string]any)
	if !ok {
		t.Fatalf("body type = %T, want map[string]any", client.gotBody)
	}
	if got, want := body["playbook_id"], "service_story_citation"; got != want {
		t.Fatalf("playbook_id = %v, want %v (trimmed)", got, want)
	}
	if got, want := len(body), 2; got != want {
		t.Fatalf("body has %d keys (%v), want exactly %d: playbook_id and inputs", got, body, want)
	}
	if envelope.Data.Resolved.PlaybookID != "service_story_citation" {
		t.Fatalf("decoded envelope = %+v, want the filled resolved playbook id", envelope)
	}
}

func TestRunResolveClientErrorPropagatesUnchangedWithNoOutput(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("connection refused")
	client := &fakeClient{err: wantErr}
	var out strings.Builder
	err := RunResolve(&out, client, ResolveOptions{PlaybookID: "service_story_citation"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunResolve error = %v, want %v", err, wantErr)
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty on transport failure", out.String())
	}
}

// A nil client is a wiring bug in the caller. The seam must fail loudly rather
// than succeed silently or panic, so an unwired wrapper cannot look healthy.
func TestNilClientRecordsFailure(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	if err := RunList(&out, nil); err == nil {
		t.Fatal("RunList(nil client) = nil error, want failure")
	}
	if err := RunResolve(&out, nil, ResolveOptions{PlaybookID: "x"}); err == nil {
		t.Fatal("RunResolve(nil client) = nil error, want failure")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty when client is nil", out.String())
	}
}
