// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestResolveGenericCalleeUsesLanguageResolverBeforeRepoUniqueName(t *testing.T) {
	previous := codeCallLanguageResolvers
	t.Cleanup(func() {
		codeCallLanguageResolvers = previous
	})
	codeCallLanguageResolvers = map[string][]shared.Resolver{
		"fixture": {
			{
				Phase: shared.PhaseBeforeRepoFallback,
				Resolve: func(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
					if ctx.CallName() != "Target" {
						return "", "", ""
					}
					return "fixture-target", "fixture/target.fixture", codeprovenance.MethodTypeInferred
				},
			},
		},
	}

	// A real repo-unique "Target" competes with the language resolver, so the
	// test proves resolver precedence rather than an empty fallback.
	index := shared.BuildEntityIndex([]facts.Envelope{
		goSourceFileEnvelope("repo-1", "target/target.go", "Target", "repo-unique-target"),
	})
	if got := index.UniqueNameByRepo("repo-1", "Target"); got != "repo-unique-target" {
		t.Fatalf("fixture repo-unique Target = %q, want repo-unique-target", got)
	}
	call := map[string]any{
		"lang": "fixture",
		"name": "Target",
	}

	entityID, calleeFile, method := resolveGenericCallee(
		index,
		"repo-1",
		nil,
		shared.ReexportIndex{},
		"caller.fixture",
		"caller.fixture",
		map[string]any{"lang": "fixture"},
		call,
	)
	if entityID != "fixture-target" {
		t.Fatalf("entityID = %q, want fixture-target", entityID)
	}
	if calleeFile != "fixture/target.fixture" {
		t.Fatalf("calleeFile = %q, want fixture/target.fixture", calleeFile)
	}
	if method != codeprovenance.MethodTypeInferred {
		t.Fatalf("method = %q, want %q", method, codeprovenance.MethodTypeInferred)
	}
}
