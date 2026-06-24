// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"testing"
)

func TestDefaultRegistryBuilds(t *testing.T) {
	t.Parallel()
	r := Default()
	if len(r.Entries()) == 0 {
		t.Fatal("Default registry is empty")
	}
}

func TestDefaultRegistryIncludesScopedTokenRegistryFile(t *testing.T) {
	t.Parallel()
	r := Default()

	entry, ok := r.Lookup("ESHU_SCOPED_TOKENS_FILE")
	if !ok {
		t.Fatal("ESHU_SCOPED_TOKENS_FILE missing from default registry")
	}
	if entry.Type != VarString {
		t.Fatalf("ESHU_SCOPED_TOKENS_FILE type = %q, want %q", entry.Type, VarString)
	}
	if entry.Subsystem != "api" {
		t.Fatalf("ESHU_SCOPED_TOKENS_FILE subsystem = %q, want api", entry.Subsystem)
	}
}

func TestValidateInvalidValuesAreErrors(t *testing.T) {
	t.Parallel()
	r := Default()

	env := map[string]string{
		"ESHU_POSTGRES_MAX_OPEN_CONNS": "thirty", // not an int
		"ESHU_POSTGRES_PING_TIMEOUT":   "10",     // missing duration unit
		"ESHU_GRAPH_BACKEND":           "sqlite", // not an allowed enum value
		"ESHU_QUERY_PROFILE":           "production",
	}
	findings := r.Validate(env, false)

	wantErr := map[string]bool{
		"ESHU_POSTGRES_MAX_OPEN_CONNS": true,
		"ESHU_POSTGRES_PING_TIMEOUT":   true,
		"ESHU_GRAPH_BACKEND":           true,
	}
	gotErr := map[string]bool{}
	for _, f := range findings {
		if f.Kind == FindingInvalidValue {
			if !f.Error {
				t.Errorf("invalid-value finding for %s should be an error", f.Name)
			}
			gotErr[f.Name] = true
		}
	}
	for name := range wantErr {
		if !gotErr[name] {
			t.Errorf("expected invalid-value finding for %s, got none", name)
		}
	}
	if gotErr["ESHU_QUERY_PROFILE"] {
		t.Error("ESHU_QUERY_PROFILE=production is valid and should not be flagged")
	}
}

func TestValidateDeprecatedAliasWarns(t *testing.T) {
	t.Parallel()
	r := Default()

	findings := r.Validate(map[string]string{"ESHU_REDUCER_CLAIM_DOMAIN": "code"}, false)
	var found bool
	for _, f := range findings {
		if f.Name == "ESHU_REDUCER_CLAIM_DOMAIN" && f.Kind == FindingDeprecated {
			found = true
			if f.Error {
				t.Error("deprecated finding should be a warning, not an error")
			}
		}
	}
	if !found {
		t.Fatal("expected a deprecated finding for ESHU_REDUCER_CLAIM_DOMAIN")
	}
}

func TestValidateAliasIsAccepted(t *testing.T) {
	t.Parallel()
	r := Default()

	// Setting the legacy enable-claims alias is valid (not unknown).
	findings := r.Validate(map[string]string{"ESHU_WORKFLOW_COORDINATOR_ENABLE_CLAIMS": "true"}, true)
	for _, f := range findings {
		if f.Kind == FindingUnknown {
			t.Errorf("alias should be recognized, got unknown finding: %s", f.Message)
		}
	}
}

func TestValidateUnknownTypoSuggestsKnownName(t *testing.T) {
	t.Parallel()
	r := Default()

	// A near-miss of a registered variable should be flagged even in non-strict
	// mode, with a suggestion.
	findings := r.Validate(map[string]string{"ESHU_POSTGRES_DSNN": "x"}, false)
	var found bool
	for _, f := range findings {
		if f.Name == "ESHU_POSTGRES_DSNN" && f.Kind == FindingUnknown {
			found = true
		}
	}
	if !found {
		t.Fatal("expected an unknown-with-suggestion finding for ESHU_POSTGRES_DSNN")
	}
}

func TestValidateUnknownOutOfScopeSilentByDefault(t *testing.T) {
	t.Parallel()
	r := Default()

	// A legitimate out-of-scope collector variable must not be flagged unless
	// strict mode is requested, to avoid noise. Container-registry credential
	// variables are an out-of-scope example: real ESHU_* names that are not
	// registered here.
	outOfScope := "ESHU_ACR_OCI_PASSWORD"
	if got := r.Validate(map[string]string{outOfScope: "secret"}, false); len(got) != 0 {
		t.Fatalf("non-strict validate flagged out-of-scope var: %+v", got)
	}
	strict := r.Validate(map[string]string{outOfScope: "secret"}, true)
	var found bool
	for _, f := range strict {
		if f.Name == outOfScope && f.Kind == FindingUnknown {
			found = true
		}
	}
	if !found {
		t.Fatal("strict validate should flag the out-of-scope variable")
	}
}

func TestNewRejectsDuplicateName(t *testing.T) {
	t.Parallel()
	_, err := New([]Entry{
		{Name: "ESHU_X", Type: VarString, Subsystem: "runtime"},
		{Name: "ESHU_X", Type: VarString, Subsystem: "runtime"},
	})
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}
