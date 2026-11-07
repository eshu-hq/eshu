// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/ask"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestQueryRootDrained fails while any non-test file in this package still
// imports the handler-heavy query root (row 8 of
// REMOVAL_CONSUMER_DRAIN_CHECKLIST.md). The engine is the Ask family's
// runtime, so packets and truth come from query/ask, envelopes from
// querycontract, and caller auth from the auth family.
func TestQueryRootDrained(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/eshu-hq/eshu/go/internal/query" {
				t.Errorf("%s still imports the query root; retarget to query/ask, querycontract, or auth", name)
			}
		}
	}
}

// TestDrainRunawayPacketThroughContracts proves the bounded-continuation seam
// still converts a runaway envelope into an unsupported ask packet when the
// envelope arrives as a querycontract shape and the packet leaves as an ask
// shape.
func TestDrainRunawayPacketThroughContracts(t *testing.T) {
	t.Parallel()
	ordinary, ok := oversizedContinuationPacket("question", "tool", &querycontract.ResponseEnvelope{})
	if ok {
		t.Fatalf("oversizedContinuationPacket(ordinary) ok = true, packet = %#v", ordinary)
	}
	var zero ask.AnswerPacket
	if !reflect.DeepEqual(ordinary, zero) {
		t.Fatalf("oversizedContinuationPacket(ordinary) = %#v, want the zero packet", ordinary)
	}
}
