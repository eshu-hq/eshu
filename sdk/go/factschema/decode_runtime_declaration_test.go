// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	runtimedeclarationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1"
)

func runtimeDeclarationEnvelope(payload map[string]any) Envelope {
	return Envelope{FactKind: FactKindRuntimeDeclarationVersion, SchemaVersion: "1.0.0", Payload: payload}
}

// TestDecodeRuntimeDeclarationVersion_RoundTrip proves a range row, which
// leaves the version parts nil, survives the envelope payload shape unchanged.
func TestDecodeRuntimeDeclarationVersion_RoundTrip(t *testing.T) {
	t.Parallel()

	lower, upper := ">=18", "<21"
	yes, no := true, false
	want := runtimedeclarationv1.Version{
		RepoID: "github.com/eshu-hq/eshu", RelativePath: "services/api/.tool-versions", ScopeDir: "services/api",
		LineNumber: 2, OrderIndex: 0, SourceKind: "tool_versions", Runtime: "node", RawSpec: ">=18 <21",
		SpecKind:   runtimedeclarationv1.SpecKindRange,
		RangeLower: &lower, RangeUpper: &upper,
		RangeLowerMajor: intPtr(18), RangeUpperMajor: intPtr(21),
		RangeLowerInclusive: &yes, RangeUpperInclusive: &no,
	}
	payload, err := EncodeRuntimeDeclarationVersion(want)
	if err != nil {
		t.Fatalf("EncodeRuntimeDeclarationVersion error = %v, want nil", err)
	}
	for _, key := range []string{"version_major", "version_minor", "version_patch", "distribution", "tool_name"} {
		if _, present := payload[key]; present {
			t.Fatalf("payload carries %q, want it absent so a range row is never zero-filled", key)
		}
	}
	got, err := DecodeRuntimeDeclarationVersion(runtimeDeclarationEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeRuntimeDeclarationVersion error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, want)
	}
}

// TestDecodeRuntimeDeclarationVersion_ToleratesUnknownSpecKind pins the
// tolerant-reader rule: a spec_kind member this build does not know decodes
// instead of rejecting the fact.
func TestDecodeRuntimeDeclarationVersion_ToleratesUnknownSpecKind(t *testing.T) {
	t.Parallel()

	payload, err := EncodeRuntimeDeclarationVersion(runtimedeclarationv1.Version{
		RepoID: "r", RelativePath: ".nvmrc", ScopeDir: ".", LineNumber: 1, OrderIndex: 0,
		SourceKind: "nvmrc", Runtime: "node", RawSpec: "20", SpecKind: "some_future_member",
	})
	if err != nil {
		t.Fatalf("EncodeRuntimeDeclarationVersion error = %v, want nil", err)
	}
	got, err := DecodeRuntimeDeclarationVersion(runtimeDeclarationEnvelope(payload))
	if err != nil {
		t.Fatalf("decode with unknown spec_kind error = %v, want nil", err)
	}
	if got.SpecKind != "some_future_member" {
		t.Fatalf("SpecKind = %q, want the unknown member preserved", got.SpecKind)
	}
}

// TestDecodeRuntimeDeclarationVersion_MissingRequiredIsInputInvalid proves a
// payload without raw_spec dead-letters as input_invalid naming the field, and
// is never zero-filled into an empty spec.
func TestDecodeRuntimeDeclarationVersion_MissingRequiredIsInputInvalid(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"repo_id": "r", "relative_path": ".nvmrc", "scope_dir": ".", "line_number": 1, "order_index": 0,
		"source_kind": "nvmrc", "runtime": "node", "spec_kind": "exact",
	}
	_, err := DecodeRuntimeDeclarationVersion(runtimeDeclarationEnvelope(payload))
	var decodeErr *DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("error = %v, want *DecodeError", err)
	}
	if decodeErr.Classification != ClassificationInputInvalid || decodeErr.Field != "raw_spec" {
		t.Fatalf("DecodeError = {%s %s}, want input_invalid on raw_spec", decodeErr.Classification, decodeErr.Field)
	}
}

// TestRuntimeDeclarationVersionRequiredSet pins the nine required fields the
// arbiter ruling fixed; a tenth required field would break every producer.
func TestRuntimeDeclarationVersionRequiredSet(t *testing.T) {
	t.Parallel()

	want := []string{
		"line_number", "order_index", "raw_spec", "relative_path", "repo_id",
		"runtime", "scope_dir", "source_kind", "spec_kind",
	}
	got := append([]string(nil), requiredPayloadKeys[runtimedeclarationv1.Version]()...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required set = %v, want %v", got, want)
	}
}
