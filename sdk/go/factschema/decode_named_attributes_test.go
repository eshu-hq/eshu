// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"testing"
)

func TestDecodeExplicitlyTaggedAttributesField(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"ami":           "ami-0abc",
		"instance_type": "t3.micro",
	}
	tests := []struct {
		name     string
		factKind string
		payload  map[string]any
		decode   func(Envelope) (map[string]any, error)
	}{
		{
			name:     "terraform state resource",
			factKind: FactKindTerraformStateResource,
			payload: map[string]any{
				"address":    "module.app.aws_instance.web",
				"attributes": want,
			},
			decode: func(env Envelope) (map[string]any, error) {
				decoded, err := DecodeTerraformStateResource(env)
				return decoded.Attributes, err
			},
		},
		{
			name:     "AWS warning",
			factKind: FactKindAWSWarning,
			payload: map[string]any{
				"account_id":   "111111111111",
				"region":       "us-east-1",
				"warning_kind": "assume_role_failed",
				"attributes":   want,
			},
			decode: func(env Envelope) (map[string]any, error) {
				decoded, err := DecodeAWSWarning(env)
				return decoded.Attributes, err
			},
		},
		{
			name:     "secrets IAM coverage warning",
			factKind: FactKindSecretsIAMCoverageWarning,
			payload: map[string]any{
				"provider":                 "vault",
				"collector_instance_id":    "collector-vault",
				"redaction_policy_version": "secrets-iam-v1",
				"warning_kind":             "vault_read_forbidden",
				"source_state":             "partial",
				"attributes":               want,
			},
			decode: func(env Envelope) (map[string]any, error) {
				decoded, err := DecodeSecretsIAMCoverageWarning(env)
				return decoded.Attributes, err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			test.payload["unknown_top_level"] = "ignored by the typed payload"
			got, err := test.decode(Envelope{
				FactKind:      test.factKind,
				SchemaVersion: "1.0.0",
				Payload:       test.payload,
			})
			if err != nil {
				t.Fatalf("decode explicitly tagged Attributes field: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Attributes = %#v, want flat named value %#v", got, want)
			}
		})
	}
}

func TestDecodeMapIntoWithDoesNotSkipNamedAttributes(t *testing.T) {
	t.Parallel()

	type namedAttributes struct {
		Name       string         `json:"name"`
		Attributes map[string]any `json:"attributes,omitempty"`
	}

	want := map[string]any{"instance_type": "t3.micro"}
	var got namedAttributes
	err := decodeMapIntoWith(
		map[string]any{
			"name":          "web",
			"attributes":    want,
			"unknown_field": "ignored",
		},
		&got,
		decodeConfig{skipAttributesRemainder: true},
	)
	if err != nil {
		t.Fatalf("decodeMapIntoWith() error = %v, want nil", err)
	}
	if got.Name != "web" {
		t.Fatalf("Name = %q, want web", got.Name)
	}
	if !reflect.DeepEqual(got.Attributes, want) {
		t.Fatalf("Attributes = %#v, want named value %#v", got.Attributes, want)
	}
}

func TestDecodeNamedAttributesValueShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		attributes any
		present    bool
		want       map[string]any
		wantError  bool
	}{
		{name: "absent"},
		{name: "explicit null", attributes: nil, present: true},
		{name: "present empty object", attributes: map[string]any{}, present: true, want: map[string]any{}},
		{name: "wrong scalar type", attributes: "not-an-object", present: true, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			payload := map[string]any{"address": "aws_instance.web"}
			if test.present {
				payload["attributes"] = test.attributes
			}
			got, err := DecodeTerraformStateResource(Envelope{
				FactKind:      FactKindTerraformStateResource,
				SchemaVersion: "1.0.0",
				Payload:       payload,
			})
			if test.wantError {
				var decodeErr *DecodeError
				if !errors.As(err, &decodeErr) {
					t.Fatalf("error = %v, want *DecodeError", err)
				}
				if decodeErr.Classification != ClassificationInputInvalid {
					t.Fatalf("classification = %q, want %q", decodeErr.Classification, ClassificationInputInvalid)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeTerraformStateResource() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(got.Attributes, test.want) {
				t.Fatalf("Attributes = %#v, want %#v", got.Attributes, test.want)
			}
		})
	}
}

func TestAttributesRemainderRequiresExactJSONExclusion(t *testing.T) {
	t.Parallel()

	type literalDashAttributes struct {
		Attributes map[string]any `json:"-,omitempty"`
	}

	want := map[string]any{"provider_specific_key": "value"}
	var got literalDashAttributes
	if err := decodeMapInto(map[string]any{"-": want, "unknown": "ignored"}, &got); err != nil {
		t.Fatalf("decodeMapInto() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got.Attributes, want) {
		t.Fatalf("Attributes = %#v, want literal-dash named value %#v", got.Attributes, want)
	}
}
