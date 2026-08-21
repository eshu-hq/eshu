// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// TestShellExecFamilyCassetteMatchesGoOdu guards against drift between the
// hand-authored live-drive JSON cassette
// (testdata/cassettes/shellexec/ifa-shell-exec-family.json, driven by
// `ifa drive` under the live lanes once wired) and the in-memory Go Odù
// (ifa's shell_exec_family_odu.go, used by the pure vacuity guard): both
// describe the SAME fixture facts, authored twice for two different
// consumers. This test proves the full normalized facts.Envelope sequence is
// identical, so non-edge-driving payload, identity, or lifecycle drift cannot
// hide behind an unchanged derived edge set.
func TestShellExecFamilyCassetteMatchesGoOdu(t *testing.T) {
	t.Parallel()
	source, err := cassette.NewSource(ifa.ShellExecFamilyCassetteFullPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}
	generation, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("cassette source Next: %v", err)
	}
	if !ok {
		t.Fatal("cassette source returned no generation")
	}

	cassetteFacts := make([]facts.Envelope, 0, generation.FactCount())
	for envelope := range generation.Facts {
		cassetteFacts = append(cassetteFacts, envelope)
	}
	compiled := ifa.CatalogByName()[ifa.ShellExecFamilyOduName]
	if compiled.Name == "" {
		t.Fatalf("CatalogByName omits %q", ifa.ShellExecFamilyOduName)
	}
	if len(cassetteFacts) != len(compiled.Facts) {
		t.Fatalf("cassette has %d facts, compiled catalog Odù has %d", len(cassetteFacts), len(compiled.Facts))
	}
	for i := range cassetteFacts {
		got := cassetteFacts[i]
		got.FactID = ""
		got.FencingToken = 0
		got.ObservedAt = time.Time{}
		got.SourceRef = facts.Ref{}
		if !reflect.DeepEqual(got, compiled.Facts[i]) {
			t.Errorf("fact %d drifted between replay cassette and catalog\ncassette: %#v\ncatalog: %#v", i, got, compiled.Facts[i])
		}
	}
	if _, ok, err := source.Next(context.Background()); err != nil || ok {
		t.Fatalf("cassette declares more than one generation: ok=%v err=%v", ok, err)
	}
}
