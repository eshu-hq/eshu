// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/payloadusage"
)

// TestPayloadUsageManifest is the reducer-side drift lock for Contract
// System v1 §6 enforcement gate 2
// (docs/internal/design/contract-system-v1.md#6-enforcement-gates): the
// payload-usage manifest, derived from the typed factschema.Decode* calls in
// factschema_decode.go, must never contain a used field this package's
// reducer handlers read that the corresponding checked-in JSON Schema
// (sdk/go/factschema/schema/*.json) does not declare. This is the reverse
// break the forward-direction factschema-diff gate (issue #4569) cannot
// catch: a reducer that starts requiring a payload field no schema promises.
//
// This test is READ-ONLY with respect to reducer business logic: it never
// edits a handler; it only asserts that go/internal/payloadusage's
// derivation, run against the real handlers in this package, finds no
// violations. All derivation and comparison logic lives in
// go/internal/payloadusage — this file exists so `go test ./internal/reducer
// -run TestPayloadUsageManifest` (this repository's gate command for issue
// #4573) has a concrete target inside the reducer package, which is where an
// operator investigating a red gate is already looking.
func TestPayloadUsageManifest(t *testing.T) {
	t.Parallel()

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	manifest, violations, err := payloadusage.Gate(payloadusage.Paths{RepoRoot: repoRoot})
	if err != nil {
		t.Fatalf("payloadusage.Gate() error = %v", err)
	}
	if len(violations) != 0 {
		msg := ""
		for _, v := range violations {
			msg += v.String() + "\n"
		}
		t.Fatalf(
			"payload-usage manifest gate found %d violation(s): a reducer handler reads a payload field its fact kind's declared schema does not cover:\n%s",
			len(violations), msg,
		)
	}
	if len(manifest.Kinds) == 0 {
		t.Fatal("payloadusage.Gate() produced an empty manifest; expected at least the 8 migrated AWS/IAM/security-group fact kinds (#4568)")
	}
}
