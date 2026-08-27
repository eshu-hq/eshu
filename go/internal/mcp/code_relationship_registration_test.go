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
	if got, want := len(codebase), 33; got != want {
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
	if got, want := len(tools), 162; got != want {
		t.Fatalf("len(ReadOnlyTools()) = %d, want %d", got, want)
	}
	if got := tools[relationshipStart : relationshipStart+len(wantRelationships)]; !reflect.DeepEqual(got, wantRelationships) {
		t.Fatal("ReadOnlyTools relationship definitions drifted from relationships.CodeTools")
	}

	const wantOrderHash = "8256c2bf64a304185a32bfb1924a6ffd8b3439e9d7d82078ba223382360aa45b"
	hash := sha256.New()
	for _, tool := range tools {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", len(tool.Name), tool.Name)
	}
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != wantOrderHash {
		t.Fatalf("ReadOnlyTools ordered-name hash = %s, want %s", got, wantOrderHash)
	}
}
