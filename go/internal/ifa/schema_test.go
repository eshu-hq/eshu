// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

func schemaBackedRegistry() map[string]facts.FactKindRegistryEntry {
	return map[string]facts.FactKindRegistryEntry{
		"aws_resource": {
			Kind:          "aws_resource",
			PayloadSchema: "sdk/go/factschema/schema/aws_resource.v1.schema.json",
		},
		"aws_tag_observation": {
			Kind:          "aws_tag_observation",
			PayloadSchema: "",
		},
	}
}

func TestValidateOduPayloadsFailsOnInvalidPayloadNamingKind(t *testing.T) {
	t.Parallel()

	invalid, ok := fixturepack.InvalidPayload("aws_resource")
	if !ok {
		t.Fatal("fixturepack has no invalid aws_resource payload fixture")
	}
	odu := Odu{
		Name: "odu:bad-aws",
		Facts: []facts.Envelope{
			{FactKind: "aws_resource", Payload: invalid},
		},
	}
	err := ValidateOduPayloads(odu, schemaBackedRegistry())
	if err == nil {
		t.Fatal("ValidateOduPayloads(invalid payload) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "aws_resource") {
		t.Errorf("error = %q, want it to name the fact kind aws_resource", err.Error())
	}
}

func TestValidateOduPayloadsPassesOnValidPayload(t *testing.T) {
	t.Parallel()

	valid, ok := fixturepack.ValidPayload("aws_resource")
	if !ok {
		t.Fatal("fixturepack has no valid aws_resource payload fixture")
	}
	odu := Odu{
		Name: "odu:good-aws",
		Facts: []facts.Envelope{
			{FactKind: "aws_resource", Payload: valid},
		},
	}
	if err := ValidateOduPayloads(odu, schemaBackedRegistry()); err != nil {
		t.Fatalf("ValidateOduPayloads(valid payload) = %v, want nil", err)
	}
}

func TestValidateOduPayloadsPassesSchemaLessRegisteredKind(t *testing.T) {
	t.Parallel()

	odu := Odu{
		Name: "odu:tag-only",
		Facts: []facts.Envelope{
			{FactKind: "aws_tag_observation", Payload: map[string]any{"key": "env", "value": "prod"}},
		},
	}
	if err := ValidateOduPayloads(odu, schemaBackedRegistry()); err != nil {
		t.Fatalf("ValidateOduPayloads(schema-less registered kind) = %v, want nil (registry-only, non-blocking)", err)
	}
}

func TestValidateOduPayloadsPassesCatalogedAwsPackOdu(t *testing.T) {
	t.Parallel()

	byKind := map[string]facts.FactKindRegistryEntry{}
	for _, entry := range facts.FactKindRegistry() {
		byKind[entry.Kind] = entry
	}
	if err := ValidateOduPayloads(awsPackOdu().Odu, byKind); err != nil {
		t.Fatalf("ValidateOduPayloads(odu:aws-pack) against the real registry = %v, want nil", err)
	}
}

func TestValidateOduPayloadsPassesUnregisteredKindUntouched(t *testing.T) {
	t.Parallel()

	odu := Odu{
		Name: "odu:content-only",
		Facts: []facts.Envelope{
			{FactKind: contentFactKind, Payload: map[string]any{"relative_path": "x.yaml", "content": "whatever: true\n"}},
		},
	}
	if err := ValidateOduPayloads(odu, schemaBackedRegistry()); err != nil {
		t.Fatalf("ValidateOduPayloads(unregistered kind) = %v, want nil", err)
	}
}
