// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func TestResolveGenericCalleeUsesLanguageResolverBeforeRepoUniqueName(t *testing.T) {
	previous := codeCallLanguageResolvers
	t.Cleanup(func() {
		codeCallLanguageResolvers = previous
	})
	codeCallLanguageResolvers = map[string][]codeCallLanguageResolver{
		"fixture": {
			{
				phase: codeCallLanguageResolverPhaseBeforeRepoFallback,
				resolve: func(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
					if ctx.callName() != "Target" {
						return "", "", ""
					}
					return "fixture-target", "fixture/target.fixture", codeprovenance.MethodTypeInferred
				},
			},
		},
	}

	index := codeEntityIndex{
		uniqueNameByRepo: map[string]map[string]string{
			"repo-1": {
				"Target": "repo-unique-target",
			},
		},
		entityFileByID: map[string]string{
			"fixture-target":     "fixture/target.fixture",
			"repo-unique-target": "fallback/target.fixture",
		},
	}
	call := map[string]any{
		"lang": "fixture",
		"name": "Target",
	}

	entityID, calleeFile, method := resolveGenericCallee(
		index,
		"repo-1",
		nil,
		codeCallReexportIndex{},
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
