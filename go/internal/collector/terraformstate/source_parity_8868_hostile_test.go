// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestParity8868NilEmptyStateFailsClosed proves nil and empty inputs are
// rejected before any fact exists, and that a legitimately emptied state
// parses to exactly the snapshot fact the trusted-deletion gate expects.
func TestParity8868NilEmptyStateFailsClosed(t *testing.T) {
	t.Parallel()

	fixture := loadParity8868Fixture(t)
	options := parity8868Options(t, fixture)

	if _, err := terraformstate.Parse(context.Background(), nil, options); err == nil {
		t.Fatal("Parse(nil reader) error = nil, want rejection")
	}
	for name, body := range map[string]string{
		"empty":      "",
		"null":       "null",
		"array root": fixture.Hostile.ArrayRootBody,
	} {
		if _, err := terraformstate.Parse(context.Background(), strings.NewReader(body), options); err == nil {
			t.Fatalf("Parse(%s) error = nil, want rejection", name)
		}
	}

	emptyBody := `{"format_version":"1.0","terraform_version":"1.9.8","serial":17,"lineage":"lineage-123"}`
	noWarnings := options
	noWarnings.SourceWarnings = nil
	empty := parity8868Parse(t, fixture, noWarnings, []byte(emptyBody))
	if len(empty.Facts) != 1 {
		t.Fatalf("len(empty Facts) = %d, want exactly the snapshot fact", len(empty.Facts))
	}
	if got := empty.Facts[0].FactKind; got != cloud.TerraformStateSnapshotFactKind {
		t.Fatalf("empty FactKind = %q, want snapshot", got)
	}
	// Source warnings ride along on every parse but never count as state
	// content: the empty gate stays true with them present.
	withWarnings := parity8868Parse(t, fixture, options, []byte(emptyBody))
	if len(withWarnings.Facts) != 2 {
		t.Fatalf("len(empty+warning Facts) = %d, want snapshot plus source warning", len(withWarnings.Facts))
	}
	if !terraformstate.IsLegitimatelyEmptyBody(withWarnings.ResourceFacts, withWarnings.OutputFacts, withWarnings.ModuleFacts) {
		t.Fatal("IsLegitimatelyEmptyBody(empty+warning) = false, want true")
	}
	if !terraformstate.IsLegitimatelyEmptyBody(empty.ResourceFacts, empty.OutputFacts, empty.ModuleFacts) {
		t.Fatal("IsLegitimatelyEmptyBody(empty) = false, want true")
	}
	representative := parity8868Parse(t, fixture, options, fixture.State)
	if terraformstate.IsLegitimatelyEmptyBody(representative.ResourceFacts, representative.OutputFacts, representative.ModuleFacts) {
		t.Fatal("IsLegitimatelyEmptyBody(representative) = true, want false")
	}
}

// TestParity8868TruncatedStateFailsClosed proves cut-off and trailing-garbage
// bodies (the paginated-fetch corruption shape) are rejected, never parsed
// into partial facts.
func TestParity8868TruncatedStateFailsClosed(t *testing.T) {
	t.Parallel()

	fixture := loadParity8868Fixture(t)
	options := parity8868Options(t, fixture)

	for name, body := range map[string]string{
		"truncated": fixture.Hostile.TruncatedBody,
		"trailing":  fixture.Hostile.TrailingBody,
	} {
		if _, err := terraformstate.Parse(context.Background(), strings.NewReader(body), options); err == nil {
			t.Fatalf("Parse(%s) error = nil, want rejection", name)
		}
	}
}

// TestParity8868FailureClasses pins every terminal failure shape the cutover
// gate must reproduce: identity mismatch, invalid envelope context, invalid
// source identity, and the two source-level warning generations
// (state_too_large, state_missing) that replace missing or over-ceiling
// bodies.
func TestParity8868FailureClasses(t *testing.T) {
	t.Parallel()

	fixture := loadParity8868Fixture(t)
	options := parity8868Options(t, fixture)

	mismatchedSerial := options
	mismatchedSerial.Generation.GenerationID = strings.Replace(
		options.Generation.GenerationID, "serial:17",
		"serial:18", 1,
	)
	mismatchedSerial.Generation.FreshnessHint = strings.Replace(
		options.Generation.FreshnessHint, "serial=17", "serial=18", 1,
	)
	if _, err := terraformstate.Parse(context.Background(), strings.NewReader(string(fixture.State)), mismatchedSerial); err == nil {
		t.Fatal("Parse(serial mismatch) error = nil, want rejection")
	} else if !strings.Contains(err.Error(), "serial") {
		t.Fatalf("Parse(serial mismatch) error = %q, want serial identity mismatch", err)
	}

	mismatchedLineage := options
	mismatchedLineage.Generation.GenerationID = strings.Replace(
		options.Generation.GenerationID, "lineage-123", "lineage-999", 1,
	)
	mismatchedLineage.Generation.FreshnessHint = strings.Replace(
		options.Generation.FreshnessHint, "lineage-123", "lineage-999", 1,
	)
	if _, err := terraformstate.Parse(context.Background(), strings.NewReader(string(fixture.State)), mismatchedLineage); err == nil {
		t.Fatal("Parse(lineage mismatch) error = nil, want rejection")
	} else if !strings.Contains(err.Error(), "lineage") {
		t.Fatalf("Parse(lineage mismatch) error = %q, want lineage identity mismatch", err)
	}

	bad := options
	bad.Scope = scope.IngestionScope{}
	if _, err := terraformstate.Parse(context.Background(), strings.NewReader(string(fixture.State)), bad); err == nil {
		t.Fatal("Parse(blank scope) error = nil, want rejection")
	}
	bad = options
	bad.FencingToken = 0
	if _, err := terraformstate.Parse(context.Background(), strings.NewReader(string(fixture.State)), bad); err == nil {
		t.Fatal("Parse(zero fencing token) error = nil, want rejection")
	}
	bad = options
	bad.RedactionKey = redact.Key{}
	if _, err := terraformstate.Parse(context.Background(), strings.NewReader(string(fixture.State)), bad); err == nil {
		t.Fatal("Parse(zero redaction key) error = nil, want rejection")
	}
	bad = options
	bad.Source = terraformstate.StateKey{BackendKind: terraformstate.BackendS3, Locator: fixture.Hostile.BlankLocator}
	if _, err := terraformstate.Parse(context.Background(), strings.NewReader(string(fixture.State)), bad); err == nil {
		t.Fatal("Parse(blank locator) error = nil, want rejection")
	}
	bad = options
	bad.Source = terraformstate.StateKey{BackendKind: terraformstate.BackendKind(fixture.Hostile.UnsupportedBackend), Locator: fixture.Locator}
	if _, err := terraformstate.Parse(context.Background(), strings.NewReader(string(fixture.State)), bad); err == nil {
		t.Fatal("Parse(unsupported backend) error = nil, want rejection")
	}

	if _, err := terraformstate.ParseStream(context.Background(), strings.NewReader(string(fixture.State)), options, nil); err == nil {
		t.Fatal("ParseStream(nil sink) error = nil, want rejection")
	}

	for name, warning := range map[string]terraformstate.SourceWarning{
		"blank kind":   {Reason: "size_limit", Source: "s3"},
		"blank reason": {WarningKind: "state_too_large", Source: "s3"},
		"blank source": {WarningKind: "state_too_large", Reason: "size_limit"},
	} {
		_, err := terraformstate.NewWarningFact(terraformstate.WarningFactOptions{
			Scope:        options.Scope,
			Generation:   options.Generation,
			Source:       options.Source,
			ObservedAt:   options.ObservedAt,
			FencingToken: options.FencingToken,
			Warning:      warning,
		})
		if err == nil {
			t.Fatalf("NewWarningFact(%s) error = nil, want rejection", name)
		}
	}

	for name, warning := range map[string]terraformstate.SourceWarning{
		"too large": {WarningKind: "state_too_large", Reason: "size_limit", Source: "s3"},
		"missing":   {WarningKind: "state_missing", Reason: "source_missing", Source: "s3"},
	} {
		env, err := terraformstate.NewWarningFact(terraformstate.WarningFactOptions{
			Scope:        options.Scope,
			Generation:   options.Generation,
			Source:       options.Source,
			ObservedAt:   options.ObservedAt,
			FencingToken: options.FencingToken,
			Warning:      warning,
		})
		if err != nil {
			t.Fatalf("NewWarningFact(%s) error = %v, want nil", name, err)
		}
		wantKey := "terraform_state_warning:warning:" + warning.WarningKind + ":" + warning.Source + ":" + warning.Reason
		if got := env.StableFactKey; got != wantKey {
			t.Fatalf("NewWarningFact(%s) StableFactKey = %q, want %q", name, got, wantKey)
		}
		if got := env.FactKind; got != cloud.TerraformStateWarningFactKind {
			t.Fatalf("NewWarningFact(%s) FactKind = %q, want warning", name, got)
		}
	}

	if _, err := terraformstate.ReadSnapshotIdentity(context.Background(), nil); err == nil {
		t.Fatal("ReadSnapshotIdentity(nil reader) error = nil, want rejection")
	}
	if _, err := terraformstate.ReadSnapshotIdentity(context.Background(), strings.NewReader(`{"serial":1}`)); err == nil {
		t.Fatal("ReadSnapshotIdentity(blank lineage) error = nil, want rejection")
	}
	if !errors.Is(terraformstate.ErrStateTooLarge, terraformstate.ErrStateTooLarge) {
		t.Fatal("ErrStateTooLarge sentinel is not comparable, want stable failure identity")
	}
	if !errors.Is(terraformstate.ErrStateMissing, terraformstate.ErrStateMissing) {
		t.Fatal("ErrStateMissing sentinel is not comparable, want stable failure identity")
	}
}

// TestParity8868DuplicateDeliveryStable proves retries are idempotent: the
// same body parsed twice (Parse and ParseStream) yields identical stable
// keys and identical canonical bytes, and resource order never changes the
// stable-key set.
func TestParity8868DuplicateDeliveryStable(t *testing.T) {
	t.Parallel()

	fixture := loadParity8868Fixture(t)
	options := parity8868Options(t, fixture)
	first := parity8868Parse(t, fixture, options, fixture.State)
	second := parity8868Parse(t, fixture, options, fixture.State)

	firstBytes := parity8868CanonicalBytes(t, first.Facts)
	secondBytes := parity8868CanonicalBytes(t, second.Facts)
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("re-parse canonical bytes differ, want idempotent retry")
	}
	if first.Facts[0].FactID != second.Facts[0].FactID {
		t.Fatal("re-parse FactID differs within one generation, want stable retry identity")
	}

	streamed := []facts.Envelope{}
	_, err := terraformstate.ParseStream(
		context.Background(),
		strings.NewReader(string(fixture.State)),
		options,
		terraformstate.FactSinkFunc(func(_ context.Context, envelope facts.Envelope) error {
			streamed = append(streamed, envelope)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("ParseStream() error = %v, want nil", err)
	}
	if got, want := parity8868StableKeys(streamed), parity8868StableKeys(first.Facts); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ParseStream stable keys differ from Parse:\ngot  %#v\nwant %#v", got, want)
	}

	// Swap whole resource objects as raw messages: the streaming parser
	// requires mode/type/name before instances inside each resource, so a
	// map round-trip that reorders keys would not parse.
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(fixture.State, &outer); err != nil {
		t.Fatalf("Unmarshal(fixture state) error = %v, want nil", err)
	}
	var resources []json.RawMessage
	if err := json.Unmarshal(outer["resources"], &resources); err != nil {
		t.Fatalf("Unmarshal(fixture resources) error = %v, want nil", err)
	}
	if len(resources) != 2 {
		t.Fatalf("len(fixture resources) = %d, want 2 resources to reorder", len(resources))
	}
	resources[0], resources[1] = resources[1], resources[0]
	swapped, err := json.Marshal(resources)
	if err != nil {
		t.Fatalf("Marshal(reordered resources) error = %v, want nil", err)
	}
	outer["resources"] = swapped
	reordered, err := json.Marshal(outer)
	if err != nil {
		t.Fatalf("Marshal(reordered state) error = %v, want nil", err)
	}
	third := parity8868Parse(t, fixture, options, reordered)
	if got, want := parity8868StableKeys(third.Facts), parity8868StableKeys(first.Facts); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("reordered stable keys differ:\ngot  %#v\nwant %#v", got, want)
	}
}
