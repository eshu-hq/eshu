// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"strings"
	"testing"
)

// TestCarriesUserinfoReadsTheOpaqueSpelling sweeps the userinfo spellings a
// caller can hand a sanitizer, varying the character in front of the
// credential position, and pins the answer for each. The opaque rows are the
// defect class this helper exists for: `svc:PASSWORD@h.internal:5432/tool`
// parses with User == nil, so a plain parsed.User test read a password in
// plain sight as no credential at all.
func TestCarriesUserinfoReadsTheOpaqueSpelling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "hierarchical userinfo", value: "https://svc:pw@h.internal/tool", want: true},
		{name: "hierarchical username only", value: "https://svc@h.internal/tool", want: true},
		{name: "hierarchical without userinfo", value: "https://h.internal/tool", want: false},
		{name: "opaque password position", value: "svc:pw@h.internal:5432/tool", want: true},
		{name: "opaque under an uppercase scheme", value: "SVC:pw@h.internal/tool", want: true},
		{name: "operator omitted the scheme entirely", value: "h.internal:5432/tool", want: false},
		{name: "scheme-relative userinfo", value: "//svc:pw@h.internal/tool", want: true},
		{name: "mailto is the same shape as a password", value: "mailto:user@example.com", want: true},
		{name: "opaque tool name without an at sign", value: "mcp:tool/name", want: false},
		{name: "purl keeps its version at sign after the slash", value: "pkg:npm/lodash@4.17.21", want: false},
		{name: "opaque at sign after the first slash", value: "svc:name/dev@example.com", want: false},
		{name: "relative path with an at sign", value: "dev@example.com/services", want: false},
		{name: "at sign inside a hierarchical path segment", value: "https://h.internal/owners/dev@example.com/x", want: false},
		{name: "unparseable value fails closed", value: "https://svc:pw\"x@h.internal/tool", want: true},
		{name: "unparseable only under the authority reading", value: "svc:pw]x@h.internal/tool", want: true},
		{name: "empty string", value: "", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := CarriesUserinfo(tt.value); got != tt.want {
				t.Errorf("CarriesUserinfo(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestAuthorityRewritesOnlyAuthorityShapedOpaques pins when the "//" re-parse
// happens at all: an opaque body with no "@" ahead of its first "/" must come
// back unrewritten, because reading `mcp:tool/name` as an authority turns
// "mcp" into a host and "tool" into an invalid port, refusing a value that
// was never a credential.
func TestAuthorityRewritesOnlyAuthorityShapedOpaques(t *testing.T) {
	t.Parallel()

	parsed, rewritten, err := Authority("mcp:tool/name")
	if err != nil {
		t.Fatalf("Authority(mcp:tool/name) error = %v, want nil", err)
	}
	if rewritten {
		t.Fatalf("Authority(mcp:tool/name) rewritten = true, want false: an opaque body without an authority must not be re-read as one")
	}
	if parsed.User != nil {
		t.Errorf("Authority(mcp:tool/name).User = %v, want nil", parsed.User)
	}

	parsed, rewritten, err = Authority("svc:pw@h.internal:5432/tool")
	if err != nil {
		t.Fatalf("Authority(svc:pw@h.internal:5432/tool) error = %v, want nil", err)
	}
	if !rewritten {
		t.Fatalf("Authority(svc:pw@h.internal:5432/tool) rewritten = false, want true")
	}
	if parsed.User == nil {
		t.Fatalf("Authority(svc:pw@h.internal:5432/tool).User = nil, want the userinfo net/url reads from the authority spelling")
	}
	if got, want := parsed.User.String(), "svc:pw"; got != want {
		t.Errorf("Authority userinfo = %q, want %q", got, want)
	}
	if got, want := parsed.Host, "h.internal:5432"; got != want {
		t.Errorf("Authority host = %q, want %q", got, want)
	}
}

// TestAuthorityRefusesWhatItCannotParse pins the fail-closed edge: a value
// that only becomes unparseable once its authority is read as an authority
// still errors instead of passing, and the error never repeats the value.
func TestAuthorityRefusesWhatItCannotParse(t *testing.T) {
	t.Parallel()

	const sentinel = "CANARY-SECRET-6119"
	_, _, err := Authority("svc:" + sentinel + "]@h.internal/tool")
	if err == nil {
		t.Fatal("Authority() error = nil, want non-nil for a value unparseable under the authority reading")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Errorf("Authority() error %q repeats the value; these errors reach logs", err)
	}
}
