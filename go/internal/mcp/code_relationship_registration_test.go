// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"

	relationshiptools "github.com/eshu-hq/eshu/go/internal/mcp/relationships"
)

func TestCodeRelationshipDefinitionsKeepProductionPositions(t *testing.T) {
	t.Parallel()

	wantRelationships := relationshiptools.CodeTools()
	codebase := codebaseTools()
	if got, want := len(codebase), 35; got != want {
		t.Fatalf("len(codebaseTools()) = %d, want %d", got, want)
	}
	const relationshipStart = 8
	if got, want := codebase[relationshipStart-1].Name, "investigate_hardcoded_secrets"; got != want {
		t.Fatalf("code relationship predecessor = %q, want %q", got, want)
	}
	if got, want := codebase[relationshipStart+len(wantRelationships)].Name, "find_dead_code"; got != want {
		t.Fatalf("code relationship successor = %q, want %q", got, want)
	}
	if got := codebase[relationshipStart : relationshipStart+len(wantRelationships)]; !reflect.DeepEqual(got, wantRelationships) {
		t.Fatal("codebaseTools relationship definitions drifted from relationships.CodeTools")
	}

	tools := ReadOnlyTools()
	if got, want := len(tools), 164; got != want {
		t.Fatalf("len(ReadOnlyTools()) = %d, want %d", got, want)
	}
	if got := tools[relationshipStart : relationshipStart+len(wantRelationships)]; !reflect.DeepEqual(got, wantRelationships) {
		t.Fatal("ReadOnlyTools relationship definitions drifted from relationships.CodeTools")
	}

	const wantOrderHash = "99e5ce09eabe1aad115e337eaf8e7d1a719d4a906b7fb054584fa3ca0fb423e0"
	hash := sha256.New()
	for _, tool := range tools {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", len(tool.Name), tool.Name)
	}
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != wantOrderHash {
		t.Fatalf("ReadOnlyTools ordered-name hash = %s, want %s", got, wantOrderHash)
	}
}
