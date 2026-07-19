// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractCodeCallRowsResolvesDartReceiverQualifiedBareFallback is the P1
// regression pin (#5332): the Dart AST rewrite emits receiver-qualified
// full_names ("repository.create") for ordinary instance-variable receivers,
// not just class references. Before the fix, codeCallExactCandidateNames only
// produced the qualified candidate ("repository.create"), which never matches
// a real declaration (the receiver is a local variable, not a scope), and the
// bare "create" fallback the old byte-scanner relied on was suppressed by
// codeCallHasQualifiedScope gating out the broad-candidate loop. The CALLS
// edge was silently dropped. This must resolve via the repo-unique bare name.
func TestExtractCodeCallRowsResolvesDartReceiverQualifiedBareFallback(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/service.dart"
	calleePath := "/repo/lib/order_repository.dart"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-dart",
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/service.dart",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "run", "line_number": 4, "end_line": 6, "uid": "uid:caller"},
				},
				"function_calls": []any{
					map[string]any{
						"name":        "create",
						"full_name":   "repository.create",
						"line_number": 5,
						"lang":        "dart",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/order_repository.dart",
			"parsed_file_data": map[string]any{
				"path": calleePath,
				"functions": []any{
					map[string]any{
						"name":          "create",
						"class_context": "OrderRepository",
						"line_number":   3,
						"end_line":      5,
						"uid":           "uid:order-repository-create",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	assertCodeCallRow(t, rows, "uid:caller", "uid:order-repository-create")
	if got := resolutionMethodForCallee(t, rows, "uid:order-repository-create"); got != codeprovenance.MethodRepoUniqueName {
		t.Fatalf("resolution_method = %q, want %q", got, codeprovenance.MethodRepoUniqueName)
	}
}

// TestExtractCodeCallRowsPrefersDartQualifiedClassReceiverOverAmbiguousBareDecoy
// is the precision pin for the same fix: a class-qualified Dart full_name
// ("Point.origin") must resolve to its exact qualified match and MUST NOT
// fall back to an unrelated same-named bare declaration elsewhere in the repo
// ("Legacy.origin") just because the bare name candidate now exists in the
// list. The qualified primary is tried first and wins; the bare fallback
// never fires here because the bare name "origin" is ambiguous across the
// repo (excluded from uniqueNameByRepo by the index-build step).
func TestExtractCodeCallRowsPrefersDartQualifiedClassReceiverOverAmbiguousBareDecoy(t *testing.T) {
	t.Parallel()

	callerPath := "/repo/lib/service.dart"
	pointPath := "/repo/lib/point.dart"
	legacyPath := "/repo/lib/legacy.dart"
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-dart",
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/service.dart",
			"parsed_file_data": map[string]any{
				"path": callerPath,
				"functions": []any{
					map[string]any{"name": "run", "line_number": 4, "end_line": 6, "uid": "uid:caller"},
				},
				"function_calls": []any{
					map[string]any{
						"name":        "origin",
						"full_name":   "Point.origin",
						"line_number": 5,
						"lang":        "dart",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/point.dart",
			"parsed_file_data": map[string]any{
				"path": pointPath,
				"functions": []any{
					map[string]any{
						"name":          "origin",
						"class_context": "Point",
						"line_number":   1,
						"end_line":      2,
						"uid":           "uid:point-origin",
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-dart",
			"relative_path": "lib/legacy.dart",
			"parsed_file_data": map[string]any{
				"path": legacyPath,
				"functions": []any{
					map[string]any{
						"name":          "origin",
						"class_context": "Legacy",
						"line_number":   1,
						"end_line":      2,
						"uid":           "uid:legacy-origin",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d; rows=%#v", got, want, rows)
	}
	assertCodeCallRow(t, rows, "uid:caller", "uid:point-origin")
	assertNoCodeCallRow(t, rows, "uid:caller", "uid:legacy-origin")
}

// TestCodeCallExactCandidateNamesDartReceiverFallback is the edge-case table
// for the new Dart candidate branch in codeCallExactCandidateNames: every
// qualified Dart full_name produces [qualified, bare-trailing-name] in that
// order, regardless of how codeCallDartQualifiedClassReceiver classifies the
// receiver segment (class vs. instance-variable vs. keyword vs. multi-segment
// vs. unrecognized) — Dart fails open and always appends the bare fallback.
func TestCodeCallExactCandidateNamesDartReceiverFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fullName string
		want     []string
	}{
		{
			name:     "lowercase instance receiver",
			fullName: "repository.create",
			want:     []string{"repository.create", "create"},
		},
		{
			name:     "uppercase class/named-constructor receiver",
			fullName: "Point.origin",
			want:     []string{"Point.origin", "origin"},
		},
		{
			name:     "leading underscore, lowercase receiver after strip",
			fullName: "_repository.save",
			want:     []string{"_repository.save", "save"},
		},
		{
			name:     "leading underscore, uppercase receiver after strip",
			fullName: "_PrivateCache.instance",
			want:     []string{"_PrivateCache.instance", "instance"},
		},
		{
			name:     "keyword receiver",
			fullName: "super.dispose",
			want:     []string{"super.dispose", "dispose"},
		},
		{
			name:     "multi-segment receiver",
			fullName: "a.b.c",
			want:     []string{"a.b.c", "c"},
		},
		{
			name:     "no-alpha unrecognized receiver fails open",
			fullName: "_.create",
			want:     []string{"_.create", "create"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			call := map[string]any{
				"name":      codeCallTrailingName(tt.fullName),
				"full_name": tt.fullName,
			}
			got := codeCallExactCandidateNames(call, "dart")
			if len(got) != len(tt.want) {
				t.Fatalf("codeCallExactCandidateNames(%q) = %#v, want %#v", tt.fullName, got, tt.want)
			}
			for i, name := range tt.want {
				if got[i] != name {
					t.Fatalf("codeCallExactCandidateNames(%q)[%d] = %q, want %q (full: %#v)", tt.fullName, i, got[i], name, got)
				}
			}
		})
	}
}

// TestCodeCallDartQualifiedClassReceiver documents and pins the
// classification codeCallExactCandidateNames' doc comment relies on: an
// UpperCamelCase qualifier (after stripping a leading "_" or "$") is a
// class/static/named-constructor reference; everything else (lowercase,
// keyword, multi-segment, or unrecognized) is treated as an instance-variable
// receiver. The candidate list itself does not branch on this — see
// TestCodeCallExactCandidateNamesDartReceiverFallback — but the classifier
// must still be independently correct.
func TestCodeCallDartQualifiedClassReceiver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fullName string
		want     bool
	}{
		{"repository.create", false},
		{"Point.origin", true},
		{"_repository.save", false},
		{"_PrivateCache.instance", true},
		{"super.dispose", false},
		{"this.dispose", false},
		{"a.b.c", false},
		{"_.create", false},
		{"create", false},
	}

	for _, tt := range tests {
		if got := codeCallDartQualifiedClassReceiver(tt.fullName); got != tt.want {
			t.Fatalf("codeCallDartQualifiedClassReceiver(%q) = %v, want %v", tt.fullName, got, tt.want)
		}
	}
}

// TestExtractCodeCallRowsOtherLanguageExactCandidatesUnaffectedByDartBranch
// guards the new language == "dart" branch does not widen matching for other
// languages: a Python receiver-qualified call with a lowercase (non-class)
// receiver must still fail to resolve (Python's fallback stays gated to
// class-style receivers only; it does not fail open like Dart's).
func TestExtractCodeCallRowsOtherLanguageExactCandidatesUnaffectedByDartBranch(t *testing.T) {
	t.Parallel()

	call := map[string]any{
		"name":      "create",
		"full_name": "repository.create",
	}
	got := codeCallExactCandidateNames(call, "python")
	want := []string{"repository.create"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("codeCallExactCandidateNames(python) = %#v, want %#v", got, want)
	}
}
