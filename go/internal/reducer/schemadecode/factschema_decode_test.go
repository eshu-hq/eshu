// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// validCodeownersOwnershipPayload is a well-formed codeowners.ownership
// payload carrying every field ownership.go documents as required (repo_id,
// source_path, pattern, owners, order_index).
func validCodeownersOwnershipPayload() map[string]any {
	return map[string]any{
		"repo_id":     "repo-123",
		"source_path": ".github/CODEOWNERS",
		"pattern":     "*.go",
		"owners":      []string{"@org/team"},
		"order_index": 0,
	}
}

// TestDecodeCodeownersOwnershipSuccess proves DecodeCodeownersOwnership's
// success path: a well-formed envelope decodes into the typed
// codeownersv1.Ownership struct with no error, and the values round-trip
// exactly (not zero values that would pass a nil-error-only check).
func TestDecodeCodeownersOwnershipSuccess(t *testing.T) {
	t.Parallel()

	env := facts.Envelope{
		FactID:        "fact-1",
		FactKind:      facts.CodeownersOwnershipFactKind,
		SchemaVersion: facts.CodeownersSchemaVersionV1,
		Payload:       validCodeownersOwnershipPayload(),
	}

	ownership, err := DecodeCodeownersOwnership(env)
	if err != nil {
		t.Fatalf("DecodeCodeownersOwnership() error = %v, want nil", err)
	}

	if got, want := ownership.RepoID, "repo-123"; got != want {
		t.Errorf("RepoID = %q, want %q", got, want)
	}
	if got, want := ownership.SourcePath, ".github/CODEOWNERS"; got != want {
		t.Errorf("SourcePath = %q, want %q", got, want)
	}
	if got, want := ownership.Pattern, "*.go"; got != want {
		t.Errorf("Pattern = %q, want %q", got, want)
	}
	if len(ownership.Owners) != 1 || ownership.Owners[0] != "@org/team" {
		t.Errorf("Owners = %v, want [@org/team]", ownership.Owners)
	}
	if got, want := ownership.OrderIndex, 0; got != want {
		t.Errorf("OrderIndex = %d, want %d", got, want)
	}
}

// TestDecodeCodeownersOwnershipMalformedPayload pins the seam's error
// contract at the package boundary: a payload missing a required field
// (repo_id) must come back as an error that unwraps to a
// *factdecode.FactDecodeError wrapping a *factschema.DecodeError classified
// input_invalid and naming the missing field — the shape
// factdecode.PartitionDecodeFailures relies on to quarantine the fact
// (rather than dead-letter the whole batch) and the shape #6372's compat
// forwarders currently launder transitively through the reducer root. This is
// the failure path that matters: nothing else in this package asserts it
// directly, since schemadecode ships with zero tests of its own today and
// this contract is otherwise exercised only incidentally through callers.
func TestDecodeCodeownersOwnershipMalformedPayload(t *testing.T) {
	t.Parallel()

	payload := validCodeownersOwnershipPayload()
	delete(payload, "repo_id")

	env := facts.Envelope{
		FactID:        "fact-2",
		FactKind:      facts.CodeownersOwnershipFactKind,
		SchemaVersion: facts.CodeownersSchemaVersionV1,
		Payload:       payload,
	}

	ownership, err := DecodeCodeownersOwnership(env)
	if err == nil {
		t.Fatalf("DecodeCodeownersOwnership() error = nil, want an error for a payload missing repo_id; got %+v", ownership)
	}

	var factDecodeErr *factdecode.FactDecodeError
	if !errors.As(err, &factDecodeErr) {
		t.Fatalf("DecodeCodeownersOwnership() error = %v (%T), want it to unwrap to *factdecode.FactDecodeError", err, err)
	}
	if factDecodeErr.Retryable() {
		t.Error("FactDecodeError.Retryable() = true, want false: a missing required field can never succeed on replay")
	}
	if got, want := factDecodeErr.FailureClass(), factschema.ClassificationInputInvalid; got != want {
		t.Errorf("FailureClass() = %q, want %q", got, want)
	}

	var decodeErr *factschema.DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("DecodeCodeownersOwnership() error = %v, want it to unwrap to *factschema.DecodeError", err)
	}
	if got, want := decodeErr.FactKind, facts.CodeownersOwnershipFactKind; got != want {
		t.Errorf("DecodeError.FactKind = %q, want %q", got, want)
	}
	if got, want := decodeErr.Field, "repo_id"; got != want {
		t.Errorf("DecodeError.Field = %q, want %q — the missing field must be named for an operator reading the dead-letter", got, want)
	}
}
