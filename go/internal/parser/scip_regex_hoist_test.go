// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"reflect"
	"testing"
)

// TestScipNameFromSymbolMatchesSeparatorSplit pins scipNameFromSymbol's output
// before and after the per-call `[/#]` split regex is hoisted to a
// package-level var (issue #4874).
func TestScipNameFromSymbolMatchesSeparatorSplit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		symbol string
		want   string
	}{
		{name: "package function", symbol: "pkg/caller#handle().", want: "handle"},
		{name: "nested package path", symbol: "scip-python python . . `mod/sub`/Class#method().", want: "method"},
		{name: "class symbol", symbol: "pkg/Widget#", want: "Widget"},
		{name: "no separators", symbol: "bareSymbol", want: "bareSymbol"},
		{name: "trailing separator falls back to original symbol", symbol: "pkg/", want: "pkg/"},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := scipNameFromSymbol(testCase.symbol)
			if got != testCase.want {
				t.Fatalf("scipNameFromSymbol(%q) = %q, want %q", testCase.symbol, got, testCase.want)
			}
		})
	}
}

// TestScipParseSignatureMatchesArgsAndReturnType pins scipParseSignature's
// output before and after the per-call signature-argument regex is hoisted to
// a package-level var (issue #4874).
func TestScipParseSignatureMatchesArgsAndReturnType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		displayName    string
		wantArgs       []string
		wantReturnType string
	}{
		{
			name:           "python style with return type",
			displayName:    "def handle(name: str, count: int = 1) -> bool",
			wantArgs:       []string{"name", "count"},
			wantReturnType: "bool",
		},
		{
			name:           "no arguments",
			displayName:    "def handle() -> None",
			wantArgs:       []string{},
			wantReturnType: "None",
		},
		{
			name:           "no parens",
			displayName:    "field value",
			wantArgs:       []string{},
			wantReturnType: "",
		},
		{
			name:           "empty display name",
			displayName:    "",
			wantArgs:       []string{},
			wantReturnType: "",
		},
		{
			name:           "splat parameter stripped of leading stars",
			displayName:    "def handle(*args, **kwargs)",
			wantArgs:       []string{"args", "kwargs"},
			wantReturnType: "",
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotArgs, gotReturnType := scipParseSignature(testCase.displayName)
			if !reflect.DeepEqual(gotArgs, testCase.wantArgs) {
				t.Fatalf("scipParseSignature(%q) args = %#v, want %#v", testCase.displayName, gotArgs, testCase.wantArgs)
			}
			if gotReturnType != testCase.wantReturnType {
				t.Fatalf("scipParseSignature(%q) returnType = %q, want %q", testCase.displayName, gotReturnType, testCase.wantReturnType)
			}
		})
	}
}
