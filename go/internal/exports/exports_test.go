// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exports

import (
	"strings"
	"testing"
)

func TestScope_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		scope     Scope
		wantError string
	}{
		{
			name:      "missing identifier",
			scope:     Scope{Kind: ScopeKindRepository},
			wantError: "scope must set one of",
		},
		{
			name: "multiple identifiers",
			scope: Scope{
				Kind:          ScopeKindRepository,
				RepositoryID:  "repo-1",
				SubjectDigest: "sha256:aa",
			},
			wantError: "exactly one target identifier",
		},
		{
			name: "kind disagrees with identifier",
			scope: Scope{
				Kind:          ScopeKindImageDigest,
				RepositoryID:  "repo-1",
				SubjectDigest: "",
			},
			wantError: "requires subject_digest",
		},
		{
			name:      "blank kind",
			scope:     Scope{RepositoryID: "repo-1"},
			wantError: "scope kind must not be blank",
		},
		{
			name:      "unknown kind",
			scope:     Scope{Kind: "garbage", RepositoryID: "repo-1"},
			wantError: "unknown scope kind",
		},
		{
			name:      "valid repository scope",
			scope:     Scope{Kind: ScopeKindRepository, RepositoryID: "repo-1"},
			wantError: "",
		},
		{
			name:      "valid image scope",
			scope:     Scope{Kind: ScopeKindImageDigest, SubjectDigest: "sha256:aa"},
			wantError: "",
		},
		{
			name:      "valid package scope",
			scope:     Scope{Kind: ScopeKindPackage, PackageID: "pkg-1"},
			wantError: "",
		},
		{
			name:      "valid advisory scope",
			scope:     Scope{Kind: ScopeKindAdvisory, AdvisoryID: "GHSA-aaaa-bbbb-cccc"},
			wantError: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.scope.Validate()
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() returned unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() returned nil error, want substring %q", tc.wantError)
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tc.wantError)
			}
		})
	}
}

func TestScope_Target(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   Scope
		want string
	}{
		{"repository", Scope{Kind: ScopeKindRepository, RepositoryID: "repo-1"}, "repository:repo-1"},
		{"image digest", Scope{Kind: ScopeKindImageDigest, SubjectDigest: "sha256:aa"}, "image_digest:sha256:aa"},
		{"package", Scope{Kind: ScopeKindPackage, PackageID: "pkg-1"}, "package:pkg-1"},
		{"advisory", Scope{Kind: ScopeKindAdvisory, AdvisoryID: "GHSA-x-y-z"}, "advisory:GHSA-x-y-z"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.in.Target(); got != tc.want {
				t.Fatalf("Target() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeSeverity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want Severity
	}{
		{"CRITICAL", SeverityCritical},
		{"high", SeverityHigh},
		{"  Medium ", SeverityMedium},
		{"moderate", SeverityMedium},
		{"LOW", SeverityLow},
		{"info", SeverityNone},
		{"none", SeverityNone},
		{"", SeverityUnknown},
		{"unspecified", SeverityUnknown},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeSeverity(tc.in); got != tc.want {
				t.Fatalf("NormalizeSeverity(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFinding_RuleID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   Finding
		want string
	}{
		{
			name: "advisory wins over cve",
			in:   Finding{AdvisoryID: "GHSA-aa", CVEID: "CVE-2024-1", FindingID: "f1"},
			want: "GHSA-aa",
		},
		{
			name: "cve falls back when advisory empty",
			in:   Finding{CVEID: "CVE-2024-1", FindingID: "f1"},
			want: "CVE-2024-1",
		},
		{
			name: "finding id last resort",
			in:   Finding{FindingID: "f1"},
			want: "f1",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.in.RuleID(); got != tc.want {
				t.Fatalf("RuleID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRegistry_Export_UnsupportedFormat(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	snapshot := Snapshot{Scope: Scope{Kind: ScopeKindRepository, RepositoryID: "repo-1"}}
	err := registry.Export(discardWriter{}, FormatCycloneDXBOV, snapshot, Options{})
	if err == nil {
		t.Fatalf("Export() unsupported format returned nil error")
	}
	if !strings.Contains(err.Error(), "unsupported export format") {
		t.Fatalf("Export() error = %q, want substring %q", err.Error(), "unsupported export format")
	}
}

func TestRegistry_SupportedFormats(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	formats := registry.SupportedFormats()
	if len(formats) != 1 {
		t.Fatalf("SupportedFormats() = %v, want length 1", formats)
	}
	if formats[0] != FormatSARIF {
		t.Fatalf("SupportedFormats()[0] = %q, want %q", formats[0], FormatSARIF)
	}
}

func TestRegistry_Register_DuplicatePanics(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatalf("Register() duplicate did not panic")
		}
	}()
	registry.Register(NewSARIFExporter())
}

func TestRegistry_Register_NilPanics(t *testing.T) {
	t.Parallel()
	registry := &Registry{exporters: map[Format]Exporter{}}
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatalf("Register() nil did not panic")
		}
	}()
	registry.Register(nil)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
