// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"

	ecosystemtools "github.com/eshu-hq/eshu/go/internal/mcp/ecosystem"
)

func TestReadOnlyToolsKeepsEcosystemRegistrationPosition(t *testing.T) {
	t.Parallel()

	wantEcosystem := ecosystemtools.Tools()
	tools := ReadOnlyTools()
	if got, want := len(tools), 170; got != want {
		t.Fatalf("ReadOnlyTools count = %d, want %d", got, want)
	}
	const ecosystemStart = 44
	ecosystemEnd := ecosystemStart + len(wantEcosystem)
	if got, want := tools[ecosystemStart-1].Name, "get_repository_language_inventory"; got != want {
		t.Fatalf("ecosystem predecessor = %q, want %q", got, want)
	}
	if got, want := tools[ecosystemEnd].Name, "count_infra_resources"; got != want {
		t.Fatalf("ecosystem successor = %q, want %q", got, want)
	}
	if got := tools[ecosystemStart:ecosystemEnd]; !reflect.DeepEqual(got, wantEcosystem) {
		t.Fatal("ReadOnlyTools ecosystem definitions drifted from ecosystem.Tools")
	}

	const wantOrderHash = "d3d759ef1b93868689c66b9b821ac8c9f20ff930c5b07a82251e3f311da2b58a"
	hash := sha256.New()
	for _, tool := range tools {
		_, _ = fmt.Fprintf(hash, "%d:%s\n", len(tool.Name), tool.Name)
	}
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != wantOrderHash {
		t.Fatalf("ReadOnlyTools ordered-name hash = %s, want %s", got, wantOrderHash)
	}
}
