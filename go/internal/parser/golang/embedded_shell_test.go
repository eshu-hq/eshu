// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"reflect"
	"testing"
)

// TestGoIdentifierShadowedBeforeOffsetMatchesDeclarationForms pins
// goIdentifierShadowedBeforeOffset's output before and after the two per-call
// regex compiles are replaced by a package-level compiled-pattern cache
// (issue #4874). The cache must not change which declaration forms count as
// shadowing, and must not false-positive on an identifier that is only a
// substring of another declared name.
func TestGoIdentifierShadowedBeforeOffsetMatchesDeclarationForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		identifier string
		offset     int
		want       bool
	}{
		{
			name:       "short declaration before offset shadows",
			source:     "exec := fakeExec{}\ncall()",
			identifier: "exec",
			offset:     19,
			want:       true,
		},
		{
			name:       "var declaration before offset shadows",
			source:     "var exec fakeExec\ncall()",
			identifier: "exec",
			offset:     18,
			want:       true,
		},
		{
			name:       "no declaration does not shadow",
			source:     "call()",
			identifier: "exec",
			offset:     6,
			want:       false,
		},
		{
			name:       "declaration after offset does not shadow",
			source:     "call()\nexec := fakeExec{}",
			identifier: "exec",
			offset:     6,
			want:       false,
		},
		{
			name:       "substring identifier declaration does not shadow",
			source:     "execer := fakeExec{}\ncall()",
			identifier: "exec",
			offset:     21,
			want:       false,
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := goIdentifierShadowedBeforeOffset(testCase.source, testCase.identifier, testCase.offset)
			if got != testCase.want {
				t.Fatalf(
					"goIdentifierShadowedBeforeOffset(%q, %q, %d) = %v, want %v",
					testCase.source, testCase.identifier, testCase.offset, got, testCase.want,
				)
			}
		})
	}
}

// TestGoIdentifierShadowedBeforeOffsetCacheIsConcurrencySafe exercises
// goIdentifierShadowedBeforeOffset from multiple goroutines with overlapping
// and distinct identifiers so the -race detector can catch any unsynchronized
// access to the package-level compiled-pattern cache introduced by the regex
// hoist.
func TestGoIdentifierShadowedBeforeOffsetCacheIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	identifiers := []string{"exec", "cmdpkg", "shell", "execer"}
	done := make(chan bool, len(identifiers)*4)
	for i := 0; i < len(identifiers)*4; i++ {
		identifier := identifiers[i%len(identifiers)]
		go func(identifier string) {
			source := identifier + " := fakeExec{}\ncall()"
			done <- goIdentifierShadowedBeforeOffset(source, identifier, len(source))
		}(identifier)
	}
	for i := 0; i < len(identifiers)*4; i++ {
		if got := <-done; !got {
			t.Fatalf("goIdentifierShadowedBeforeOffset concurrent result = %v, want true", got)
		}
	}
}

// TestEmbeddedShellCommandsSkipsShadowedAliasPerFunction exercises the
// production entry point (not just the shadow-detection helper) so the regex
// hoist is proven against the real dispatch path: one function shadows the
// os/exec alias with a local variable and must be excluded, while a sibling
// function that does not shadow it is still reported.
func TestEmbeddedShellCommandsSkipsShadowedAliasPerFunction(t *testing.T) {
	t.Parallel()

	source := `package repo

import (
	"os/exec"
)

func shadowed() error {
	exec := fakeRunner{}
	return exec.Command("echo")
}

func notShadowed() error {
	cmd := exec.Command("echo")
	return cmd.Run()
}
`

	got := EmbeddedShellCommands(source)
	want := []EmbeddedShellCommand{
		{
			FunctionName:       "notShadowed",
			FunctionLineNumber: 12,
			LineNumber:         13,
			API:                "os/exec.Command",
			Language:           "go",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EmbeddedShellCommands() = %#v, want %#v", got, want)
	}
}
