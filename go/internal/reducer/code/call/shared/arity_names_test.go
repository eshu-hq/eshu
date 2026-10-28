// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"testing"
)

// TestAppendArityNames proves arity suffixes append once and invalid arity
// leaves the names untouched.
func TestAppendArityNames(t *testing.T) {
	t.Parallel()

	got := AppendArityNames([]string{"serve", "server.serve"}, 2)
	want := []string{"serve", "server.serve", "serve#2", "server.serve#2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AppendArityNames = %#v, want %#v", got, want)
	}
	again := AppendArityNames(got, 2)
	chained := []string{"serve", "server.serve", "serve#2", "server.serve#2", "serve#2#2", "server.serve#2#2"}
	if !reflect.DeepEqual(again, chained) {
		t.Fatalf("AppendArityNames twice = %#v, want %#v", again, chained)
	}
	if got := AppendArityNames([]string{"serve"}, -1); !reflect.DeepEqual(got, []string{"serve"}) {
		t.Fatalf("negative arity = %#v, want unchanged", got)
	}
	if got := AppendArityNames(nil, 2); got != nil {
		t.Fatalf("nil names = %#v, want nil", got)
	}
}

// TestAppendTypedSignatureNames proves typed signatures append once and
// blank types veto the signature.
func TestAppendTypedSignatureNames(t *testing.T) {
	t.Parallel()

	got := AppendTypedSignatureNames([]string{"serve"}, []string{"string", "int"})
	want := []string{"serve", "serve(string,int)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AppendTypedSignatureNames = %#v, want %#v", got, want)
	}
	again := AppendTypedSignatureNames(got, []string{"string", "int"})
	chained := []string{"serve", "serve(string,int)", "serve(string,int)(string,int)"}
	if !reflect.DeepEqual(again, chained) {
		t.Fatalf("AppendTypedSignatureNames twice = %#v, want %#v", again, chained)
	}
	for _, types := range [][]string{nil, {}, {"string", "  "}} {
		if got := AppendTypedSignatureNames([]string{"serve"}, types); !reflect.DeepEqual(got, []string{"serve"}) {
			t.Fatalf("types %#v left names = %#v, want unchanged", types, got)
		}
	}
	if got := AppendTypedSignatureNames(nil, []string{"string"}); got != nil {
		t.Fatalf("nil names = %#v, want nil", got)
	}
}

// TestExactCandidateNamesLanguageBranches proves the per-language exact
// candidates: JS receivers and module.exports, Dart bare fallback, Python
// class receivers, Java class-reference kinds, and arity/typed suffixes.
func TestExactCandidateNamesLanguageBranches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		call     map[string]any
		language string
		want     []string
	}{
		{
			name:     "javascript_call_receiver",
			call:     map[string]any{"name": "call", "full_name": "fn.call"},
			language: "javascript",
			want:     []string{"fn.call", "fn"},
		},
		{
			name:     "javascript_module_exports",
			call:     map[string]any{"name": "serve", "full_name": "module.exports.serve"},
			language: "javascript",
			want:     []string{"module.exports.serve", "serve"},
		},
		{
			name:     "dart_bare_fallback",
			call:     map[string]any{"name": "create", "full_name": "repository.create"},
			language: "dart",
			want:     []string{"repository.create", "create"},
		},
		{
			name:     "python_class_receiver",
			call:     map[string]any{"name": "create", "full_name": "Repository.create"},
			language: "python",
			want:     []string{"Repository.create", "create"},
		},
		{
			name:     "python_instance_receiver_no_bare",
			call:     map[string]any{"name": "create", "full_name": "repo.create"},
			language: "python",
			want:     []string{"repo.create"},
		},
		{
			name:     "java_class_reference_adds_name",
			call:     map[string]any{"name": "Config", "full_name": "com.acme.Config", "call_kind": "java.reflection_class_reference"},
			language: "java",
			want:     []string{"com.acme.Config", "Config"},
		},
		{
			name:     "class_context_and_inferred_type",
			call:     map[string]any{"name": "run", "full_name": "run", "class_context": "Worker", "inferred_obj_type": "Pool"},
			language: "go",
			want:     []string{"Worker.run", "Pool.run"},
		},
		{
			name:     "php_namespaced_inferred_type",
			call:     map[string]any{"name": "run", "full_name": "run", "inferred_obj_type": `Acme\Pool`},
			language: "php",
			want:     []string{`Acme\Pool.run`, "Pool.run"},
		},
		{
			name:     "ruby_class_context",
			call:     map[string]any{"name": "run", "full_name": "run", "context": "Worker", "context_type": "class"},
			language: "ruby",
			want:     []string{"Worker.run"},
		},
		{
			name:     "arity_and_typed_suffixes",
			call:     map[string]any{"name": "serve", "full_name": "serve", "class_context": "Worker", "argument_count": 1, "argument_types": []string{"string"}},
			language: "go",
			want:     []string{"Worker.serve", "Worker.serve#1", "Worker.serve(string)", "Worker.serve#1(string)"},
		},
		{
			name:     "bare_call_has_no_exact_candidates",
			call:     map[string]any{"name": "serve", "full_name": "serve"},
			language: "go",
			want:     []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ExactCandidateNames(tc.call, tc.language); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ExactCandidateNames = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestBroadCandidateNamesFallback proves broad names carry the trailing
// segments plus arity/typed suffixes, and Elixir stays nil.
func TestBroadCandidateNamesFallback(t *testing.T) {
	t.Parallel()

	got := BroadCandidateNames(
		map[string]any{"name": "serve", "full_name": "server.serve", "argument_count": 0},
		"javascript",
	)
	want := []string{"serve", "server.serve", "serve#0", "server.serve#0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BroadCandidateNames = %#v, want %#v", got, want)
	}
	if got := BroadCandidateNames(map[string]any{"name": "f"}, "elixir"); got != nil {
		t.Fatalf("Elixir broad names = %#v, want nil", got)
	}
	if got := BroadCandidateNames(map[string]any{}, "go"); len(got) != 0 {
		t.Fatalf("empty call broad names = %#v, want empty", got)
	}
}

// TestPythonQualifiedClassReceiver pins the UpperCamelCase receiver rule.
func TestPythonQualifiedClassReceiver(t *testing.T) {
	t.Parallel()

	for _, full := range []string{"Repository.create", " Acme.Service.run "} {
		if !PythonQualifiedClassReceiver(full) {
			t.Fatalf("PythonQualifiedClassReceiver(%q) = false, want true", full)
		}
	}
	for _, full := range []string{"", "create", "repo.create", "Repo.", ".create"} {
		if PythonQualifiedClassReceiver(full) {
			t.Fatalf("PythonQualifiedClassReceiver(%q) = true, want false", full)
		}
	}
	if !PythonQualifiedClassReceiver("pkg.Sub.Create") {
		t.Fatal("dotted receiver chain ending in a class should report true")
	}
}

// TestJavaScriptFunctionReceiverNames pins .call/.apply/.bind receiver
// extraction.
func TestJavaScriptFunctionReceiverNames(t *testing.T) {
	t.Parallel()

	if got := JavaScriptFunctionReceiverNames("fn.call"); !reflect.DeepEqual(got, []string{"fn"}) {
		t.Fatalf("receiver names = %#v, want [fn]", got)
	}
	if got := JavaScriptFunctionReceiverNames("fn.apply"); !reflect.DeepEqual(got, []string{"fn"}) {
		t.Fatalf("receiver names = %#v, want [fn]", got)
	}
	if got := JavaScriptFunctionReceiverNames("ns.fn.bind"); !reflect.DeepEqual(got, []string{"ns.fn"}) {
		t.Fatalf("receiver names = %#v, want [ns.fn]", got)
	}
	for _, full := range []string{"", "serve", "fn.callx", ".call"} {
		if got := JavaScriptFunctionReceiverNames(full); got != nil {
			t.Fatalf("receiver names(%q) = %#v, want nil", full, got)
		}
	}
}

// TestCodeCallTypeCandidateNames proves type candidate names cover name,
// full name, and context name with dedup.
func TestCodeCallTypeCandidateNames(t *testing.T) {
	t.Parallel()

	item := map[string]any{
		"name":      "Handler",
		"full_name": "server.Handler",
		"context":   []any{"App"},
	}
	got := codeCallTypeCandidateNames(item)
	want := []string{"Handler", "server.Handler", "App"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("type candidates = %#v, want %#v", got, want)
	}
	dup := map[string]any{"name": "Handler", "full_name": "Handler"}
	if got := codeCallTypeCandidateNames(dup); !reflect.DeepEqual(got, []string{"Handler"}) {
		t.Fatalf("duplicate type candidates = %#v, want [Handler]", got)
	}
	if got := codeCallTypeCandidateNames(map[string]any{}); len(got) != 0 {
		t.Fatalf("empty item type candidates = %#v, want empty", got)
	}
}

// TestCodeCallFunctionCandidateNames proves function candidate names cover
// name, full name, impl/class context, class-typed context, and parameter
// suffixes.
func TestCodeCallFunctionCandidateNames(t *testing.T) {
	t.Parallel()

	item := map[string]any{
		"name":            "Handler",
		"full_name":       "server.Handler",
		"impl_context":    "nodejs",
		"class_context":   "Server",
		"context":         "App",
		"context_type":    "class",
		"parameter_count": 1,
		"parameter_types": []string{"string"},
	}
	got := codeCallFunctionCandidateNames(item)
	for _, name := range []string{
		"Handler", "server.Handler", "nodejs::Handler", "Server.Handler",
		"App.Handler", "Handler#1", "Handler(string)",
	} {
		found := false
		for _, candidate := range got {
			if candidate == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("function candidates %#v miss %q", got, name)
		}
	}
	if got := codeCallFunctionCandidateNames(map[string]any{}); len(got) != 0 {
		t.Fatalf("empty item function candidates = %#v, want empty", got)
	}
}

// TestMetadataIntTypes proves every parser-metadata numeric shape decodes
// with negativity and range vetos.
func TestMetadataIntTypes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value any
		want  int
		ok    bool
	}{
		{"int", 3, 3, true},
		{"negative_int", -1, -1, false},
		{"int64", int64(7), 7, true},
		{"negative_int64", int64(-1), 0, false},
		{"float_whole", 4.0, 4, true},
		{"float_frac", 4.5, 0, false},
		{"float_negative", -2.0, 0, false},
		{"string", " 12 ", 12, true},
		{"string_negative", "-3", 0, false},
		{"string_garbage", "many", 0, false},
		{"bool", true, 0, false},
		{"nil", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := MetadataInt(map[string]any{"n": tc.value}, "n")
			if got != tc.want || ok != tc.ok {
				t.Fatalf("MetadataInt(%#v) = (%d, %v), want (%d, %v)", tc.value, got, ok, tc.want, tc.ok)
			}
		})
	}
	if _, ok := MetadataInt(map[string]any{}, "missing"); ok {
		t.Fatal("missing MetadataInt reported ok, want false")
	}
}

// TestMetadataStringSliceShapes proves slice, any, missing, and foreign
// metadata shapes.
func TestMetadataStringSliceShapes(t *testing.T) {
	t.Parallel()

	if got := MetadataStringSlice(map[string]any{"k": []string{"a", "b"}}, "k"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("string slice = %#v, want [a b]", got)
	}
	if got := MetadataStringSlice(map[string]any{"k": []any{" a ", "", 4}}, "k"); !reflect.DeepEqual(got, []string{"a", "4"}) {
		t.Fatalf("any slice = %#v, want [a 4]", got)
	}
	if got := MetadataStringSlice(map[string]any{"k": "a"}, "k"); got != nil {
		t.Fatalf("scalar slice = %#v, want nil", got)
	}
	if got := MetadataStringSlice(map[string]any{}, "k"); got != nil {
		t.Fatalf("missing slice = %#v, want nil", got)
	}
}

// TestCodeCallClassContexts proves nearest-to-outermost class scopes merge
// with dedup across both shapes.
func TestCodeCallClassContexts(t *testing.T) {
	t.Parallel()

	item := map[string]any{
		"class_context":            "Inner",
		"enclosing_class_contexts": []any{"Outer", "Inner", "  "},
	}
	if got := codeCallClassContexts(item); !reflect.DeepEqual(got, []string{"Inner", "Outer"}) {
		t.Fatalf("class contexts = %#v, want [Inner Outer]", got)
	}
	stringItem := map[string]any{"enclosing_class_contexts": []string{"A", "B"}}
	if got := codeCallClassContexts(stringItem); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("string class contexts = %#v, want [A B]", got)
	}
	if got := codeCallClassContexts(map[string]any{}); len(got) != 0 {
		t.Fatalf("empty class contexts = %#v, want empty", got)
	}
}
