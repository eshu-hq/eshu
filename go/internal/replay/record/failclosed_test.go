// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package record_test

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/record"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// seededTestKey is the fixed recording key for this test. A fixed seed makes
// the pseudonyms byte-stable across runs, so the test pins the fail-closed
// shape instead of merely its regex family. The material is synthetic and
// carries no customer data.
const seededTestKey = "lane1-record-seeded-test-key-0000"

// TestSeededRunIsDeterministicAndFailClosed pins the table's fail-closed
// contract end to end through the engine: a classified account keeps its
// 12-digit shape, a kept enum passes through verbatim, and a key the table
// does not list is made opaque with its path reported — identically on every
// run under the fixed seed.
func TestSeededRunIsDeterministicAndFailClosed(t *testing.T) {
	t.Parallel()

	first := runSeeded(t)
	second := runSeeded(t)

	firstJSON, err := json.Marshal(first.payload)
	if err != nil {
		t.Fatalf("marshal first payload: %v", err)
	}
	secondJSON, err := json.Marshal(second.payload)
	if err != nil {
		t.Fatalf("marshal second payload: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("seeded runs differ:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}

	account, ok := first.payload["account_id"].(string)
	if !ok {
		t.Fatalf("account_id = %T, want string", first.payload["account_id"])
	}
	if matched, _ := regexp.MatchString(`^0000[0-9]{8}$`, account); !matched {
		t.Fatalf("account_id = %q, want 0000 + 8 digits (12-digit shape preserved)", account)
	}
	if got := first.payload["resource_type"]; got != "aws_lambda_function" {
		t.Fatalf("resource_type = %v, want verbatim aws_lambda_function", got)
	}
	mystery, ok := first.payload["unlisted_customer_field"].(string)
	if !ok {
		t.Fatalf("unlisted_customer_field = %T, want string", first.payload["unlisted_customer_field"])
	}
	if matched, _ := regexp.MatchString(`^o[0-9a-f]{11}$`, mystery); !matched {
		t.Fatalf("unlisted_customer_field = %q, want opaque o + 11 hex", mystery)
	}
	if !slices.Contains(first.report.UnclassifiedPaths, "payload.unlisted_customer_field") {
		t.Fatalf("UnclassifiedPaths = %q, want it to name payload.unlisted_customer_field", first.report.UnclassifiedPaths)
	}
	if !slices.Contains(first.report.OpaquePaths, "payload.unlisted_customer_field") {
		t.Fatalf("OpaquePaths = %q, want it to name payload.unlisted_customer_field", first.report.OpaquePaths)
	}
	if first.report.KeyFingerprint == "" {
		t.Fatal("KeyFingerprint is empty, want the seed key fingerprint")
	}
}

type seededRun struct {
	payload map[string]any
	report  *recordpseudo.Report
}

func runSeeded(t *testing.T) seededRun {
	t.Helper()

	key, err := recordpseudo.NewKey([]byte(seededTestKey))
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	wrapped, report, err := recordpseudo.Wrap(newSeededSource(), key, record.Policy())
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	var payloads []map[string]any
	for {
		gen, ok, err := wrapped.Next(context.Background())
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if !ok {
			break
		}
		for env := range gen.Facts {
			payloads = append(payloads, env.Payload)
		}
	}
	if len(payloads) != 1 {
		t.Fatalf("drained %d payloads, want 1", len(payloads))
	}
	return seededRun{payload: payloads[0], report: report}
}

// seededSource is a one-generation stub source carrying a classified
// account, a kept enum, and one key the table deliberately does not list.
type seededSource struct {
	done bool
}

func newSeededSource() *seededSource { return &seededSource{} }

func (s *seededSource) Next(context.Context) (collector.CollectedGeneration, bool, error) {
	if s.done {
		return collector.CollectedGeneration{}, false, nil
	}
	s.done = true
	sc := scope.IngestionScope{ScopeID: "aws:123456789012:us-east-1:lambda"}
	gen := scope.ScopeGeneration{ScopeID: sc.ScopeID, GenerationID: "gen-1"}
	env := facts.Envelope{
		FactKind:      "aws_resource",
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"account_id":              "123456789012",
			"resource_type":           "aws_lambda_function",
			"unlisted_customer_field": "customer-secret-value",
		},
	}
	return collector.FactsFromSlice(sc, gen, []facts.Envelope{env}), true, nil
}
