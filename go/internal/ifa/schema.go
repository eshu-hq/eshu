// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

// ValidateOduPayloads validates odu's facts against the SDK's payload-schema
// conformance seam (design §1c): for every fact kind byKind registers with a
// non-blank PayloadSchema, its facts must satisfy that fixturepack schema; a
// violation is blocking and the returned error names the offending fact kind.
// A fact kind that byKind carries with a blank PayloadSchema is registry-only
// (presence-only, never payload-validated) and never blocks. A fact kind
// absent from byKind entirely (e.g. the unregistered "content" kind, #4783 W1)
// passes untouched — Ifá validates only what the registry declares a schema
// for, exactly like conformance.ValidatePayloadSchemas' own PayloadSchemas map.
func ValidateOduPayloads(odu Odu, byKind map[string]facts.FactKindRegistryEntry) error {
	rawSchemas, err := oduPayloadSchemas(odu, byKind)
	if err != nil {
		return err
	}
	if len(rawSchemas) == 0 {
		return nil
	}

	result := sdkcollector.Result{Facts: make([]sdkcollector.Fact, 0, len(odu.Facts))}
	for _, envelope := range odu.Facts {
		result.Facts = append(result.Facts, sdkcollector.Fact{
			Kind:          envelope.FactKind,
			SchemaVersion: envelope.SchemaVersion,
			StableKey:     envelope.StableFactKey,
			ObservedAt:    envelope.ObservedAt,
			Payload:       envelope.Payload,
		})
	}

	if err := conformance.ValidatePayloadSchemas(rawSchemas, result); err != nil {
		return fmt.Errorf("odu %q: %w", odu.Name, err)
	}
	return nil
}

// oduPayloadSchemas collects the fixturepack schema for every schema-backed
// fact kind actually present in odu's facts. It errors closed when the
// registry declares a PayloadSchema fixturepack does not ship — a registry/pack
// drift, not a runtime input condition — rather than silently skipping
// validation for that kind.
func oduPayloadSchemas(odu Odu, byKind map[string]facts.FactKindRegistryEntry) (map[string]json.RawMessage, error) {
	present := map[string]struct{}{}
	for _, envelope := range odu.Facts {
		present[envelope.FactKind] = struct{}{}
	}

	rawSchemas := map[string]json.RawMessage{}
	for kind := range present {
		entry, ok := byKind[kind]
		if !ok || strings.TrimSpace(entry.PayloadSchema) == "" {
			continue
		}
		schema, ok := fixturepack.SchemaFor(kind)
		if !ok {
			return nil, fmt.Errorf(
				"odu %q: fact kind %q declares payload_schema %q but fixturepack ships no schema for it",
				odu.Name, kind, entry.PayloadSchema,
			)
		}
		rawSchemas[kind] = schema
	}
	return rawSchemas, nil
}
