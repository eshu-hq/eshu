// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"testing"

	tfstatev1 "github.com/eshu-hq/eshu/sdk/go/factschema/terraformstate/v1"
)

// fullTerraformStateResourcePayload returns a valid terraform_state_resource
// payload with the required address key present, so a test can delete it and
// prove decode dead-letters on that field.
func fullTerraformStateResourcePayload() map[string]any {
	return map[string]any{
		"address":  "module.vpc.aws_subnet.public[0]",
		"mode":     "managed",
		"type":     "aws_subnet",
		"name":     "public",
		"module":   "module.vpc",
		"provider": `provider["registry.terraform.io/hashicorp/aws"]`,
	}
}

// TestDecodeTerraformStateResource_MissingAddressDeadLetters is the flagship
// contract-layer regression for the terraform_state family: a
// terraform_state_resource payload missing its required address key (absent, not
// merely empty) dead-letters as a classified input_invalid naming "address",
// never decoding to a zero-value struct. Before typing, the projector read
// address with a raw payload lookup that returned "" for the absent key and then
// dropped the resource with no operator signal; the typed seam makes the missing
// identity a visible, classified failure instead.
func TestDecodeTerraformStateResource_MissingAddressDeadLetters(t *testing.T) {
	t.Parallel()

	payload := fullTerraformStateResourcePayload()
	delete(payload, "address") // absent, not empty

	env := Envelope{FactKind: FactKindTerraformStateResource, SchemaVersion: "1.0.0", Payload: payload}
	got, err := DecodeTerraformStateResource(env)
	if err == nil {
		t.Fatalf("DecodeTerraformStateResource() error = nil, want non-nil for missing required address")
	}

	var classified *DecodeError
	if !errors.As(err, &classified) {
		t.Fatalf("DecodeTerraformStateResource() error = %T, want *DecodeError", err)
	}
	if classified.Classification != ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, ClassificationInputInvalid)
	}
	if classified.Field != "address" {
		t.Fatalf("Field = %q, want %q", classified.Field, "address")
	}

	var zero tfstatev1.Resource
	if !reflect.DeepEqual(got, zero) {
		t.Fatalf("DecodeTerraformStateResource() returned non-zero struct %+v on error, want zero value", got)
	}
}

// TestDecodeTerraformStateResource_PresentButEmptyAddressDecodes proves the
// absent-vs-empty distinction: a present-but-empty address is a VALID decode (an
// empty observed value the projector already drops as non-materializable), NOT a
// dead-letter. Flipping present-empty into input_invalid would be an accuracy
// regression the migration must not introduce.
func TestDecodeTerraformStateResource_PresentButEmptyAddressDecodes(t *testing.T) {
	t.Parallel()

	payload := fullTerraformStateResourcePayload()
	payload["address"] = "" // present, but empty

	env := Envelope{FactKind: FactKindTerraformStateResource, SchemaVersion: "1.0.0", Payload: payload}
	got, err := DecodeTerraformStateResource(env)
	if err != nil {
		t.Fatalf("DecodeTerraformStateResource() error = %v, want nil for present-but-empty address", err)
	}
	if got.Address != "" {
		t.Fatalf("Address = %q, want empty string", got.Address)
	}
}

// TestDecodeTerraformStateResource_RoundTrip proves a typed Resource encoded to a
// payload map decodes back deep-equal, including the optional pointer fields and
// the correlation_anchors slice-of-object pass-through.
func TestDecodeTerraformStateResource_RoundTrip(t *testing.T) {
	t.Parallel()

	mode := "managed"
	original := tfstatev1.Resource{
		Address: "aws_s3_bucket.logs",
		Mode:    &mode,
		CorrelationAnchors: []map[string]any{
			{"anchor_kind": "arn", "value_hash": "fp:arn:abc"},
		},
	}

	payload, err := EncodeTerraformStateResource(original)
	if err != nil {
		t.Fatalf("EncodeTerraformStateResource() error = %v, want nil", err)
	}
	decoded, err := DecodeTerraformStateResource(Envelope{FactKind: FactKindTerraformStateResource, SchemaVersion: "1.0.0", Payload: payload})
	if err != nil {
		t.Fatalf("DecodeTerraformStateResource() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeTerraformStateResource() = %+v, want %+v", decoded, original)
	}
}

// TestDecodeTerraformStateSnapshot_NoRequiredFieldDecodesEmpty proves the
// snapshot kind's deliberate zero-required-field contract: an EMPTY payload (no
// keys at all) decodes cleanly to a zero-value struct rather than dead-lettering,
// because no snapshot field's absence produces a broken graph identity (the
// projector reads every snapshot field best-effort). This is the byte-identical
// guarantee for the snapshot read path: a today-valid incomplete snapshot must
// still decode.
func TestDecodeTerraformStateSnapshot_NoRequiredFieldDecodesEmpty(t *testing.T) {
	t.Parallel()

	env := Envelope{FactKind: FactKindTerraformStateSnapshot, SchemaVersion: "1.0.0", Payload: map[string]any{}}
	got, err := DecodeTerraformStateSnapshot(env)
	if err != nil {
		t.Fatalf("DecodeTerraformStateSnapshot() error = %v, want nil for an empty snapshot payload (no required fields)", err)
	}
	var zero tfstatev1.Snapshot
	if !reflect.DeepEqual(got, zero) {
		t.Fatalf("DecodeTerraformStateSnapshot() = %+v, want zero value for empty payload", got)
	}
}

// TestDecodeTerraformStateModule_MissingModuleAddressDeadLetters proves the
// module identity guarantee: a module observation missing its required
// module_address dead-letters as input_invalid naming the field.
func TestDecodeTerraformStateModule_MissingModuleAddressDeadLetters(t *testing.T) {
	t.Parallel()

	env := Envelope{
		FactKind:      FactKindTerraformStateModule,
		SchemaVersion: "1.0.0",
		Payload:       map[string]any{"resource_count": int64(3)},
	}
	_, err := DecodeTerraformStateModule(env)
	var classified *DecodeError
	if !errors.As(err, &classified) || classified.Field != "module_address" {
		t.Fatalf("DecodeTerraformStateModule() error = %v, want *DecodeError naming module_address", err)
	}
}

// TestDecodeTerraformStateOutput_MissingNameDeadLetters proves the output
// identity guarantee: an output missing its required name dead-letters.
func TestDecodeTerraformStateOutput_MissingNameDeadLetters(t *testing.T) {
	t.Parallel()

	env := Envelope{
		FactKind:      FactKindTerraformStateOutput,
		SchemaVersion: "1.0.0",
		Payload:       map[string]any{"sensitive": false},
	}
	_, err := DecodeTerraformStateOutput(env)
	var classified *DecodeError
	if !errors.As(err, &classified) || classified.Field != "name" {
		t.Fatalf("DecodeTerraformStateOutput() error = %v, want *DecodeError naming name", err)
	}
}

// TestDecodeTerraformStateTagObservation_MissingJoinKeysDeadLetter proves BOTH
// tag→resource join keys are required: dropping either resource_address or
// tag_key_hash dead-letters on exactly that field, so a tag can never silently
// fail to join its resource.
func TestDecodeTerraformStateTagObservation_MissingJoinKeysDeadLetter(t *testing.T) {
	t.Parallel()

	full := map[string]any{
		"resource_address": "aws_subnet.public",
		"tag_key_hash":     "fp:tagkey:env:abc",
	}
	for _, field := range []string{"resource_address", "tag_key_hash"} {
		field := field
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{}
			for k, v := range full {
				payload[k] = v
			}
			delete(payload, field)
			env := Envelope{FactKind: FactKindTerraformStateTagObservation, SchemaVersion: "1.0.0", Payload: payload}
			_, err := DecodeTerraformStateTagObservation(env)
			var classified *DecodeError
			if !errors.As(err, &classified) || classified.Field != field {
				t.Fatalf("DecodeTerraformStateTagObservation() missing %q: error = %v, want *DecodeError naming %q", field, err, field)
			}
		})
	}
}

// TestDecodeTerraformStateDeferredKinds_MissingRequiredFieldDeadLetters proves
// the three typed-but-not-yet-consumed kinds still enforce their required sets
// at the seam, so the contract is correct and ready before a read consumer
// exists: candidate on path_hash, provider_binding on provider_address, warning
// on warning_kind.
func TestDecodeTerraformStateDeferredKinds_MissingRequiredFieldDeadLetters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		kind    string
		full    map[string]any
		missing string
		decode  func(Envelope) error
	}{
		{
			name: "candidate",
			kind: FactKindTerraformStateCandidate,
			full: map[string]any{
				"candidate_source": "git",
				"backend_kind":     "local",
				"repo_id":          "repo:example",
				"relative_path":    "terraform/terraform.tfstate",
				"path_hash":        "fp:path:abc",
			},
			missing: "path_hash",
			decode:  func(e Envelope) error { _, err := DecodeTerraformStateCandidate(e); return err },
		},
		{
			name: "provider_binding",
			kind: FactKindTerraformStateProviderBinding,
			full: map[string]any{
				"resource_address": "aws_subnet.public",
				"provider_address": `provider["registry.terraform.io/hashicorp/aws"]`,
			},
			missing: "provider_address",
			decode:  func(e Envelope) error { _, err := DecodeTerraformStateProviderBinding(e); return err },
		},
		{
			name: "warning",
			kind: FactKindTerraformStateWarning,
			full: map[string]any{
				"warning_kind": "tag_value_dropped",
				"reason":       "non-scalar tag value dropped",
				"source":       "resources.aws_subnet.public.attributes.tags",
			},
			missing: "warning_kind",
			decode:  func(e Envelope) error { _, err := DecodeTerraformStateWarning(e); return err },
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{}
			for k, v := range tc.full {
				payload[k] = v
			}
			delete(payload, tc.missing)
			err := tc.decode(Envelope{FactKind: tc.kind, SchemaVersion: "1.0.0", Payload: payload})
			var classified *DecodeError
			if !errors.As(err, &classified) || classified.Field != tc.missing {
				t.Fatalf("Decode %s missing %q: error = %v, want *DecodeError naming %q", tc.name, tc.missing, err, tc.missing)
			}
		})
	}
}
