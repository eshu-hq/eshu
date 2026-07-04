// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fixturepack

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// schemaFS embeds the checked-in JSON Schema for every typed fact kind. The
// files are byte-identical to sdk/go/factschema/schema/*.json; the parent
// module's drift-lock test enforces that equality.
//
//go:embed schema/*.json
var schemaFS embed.FS

// payloadFS embeds the curated example payloads. Each fact kind has one valid
// payload (payloads/<kind>.valid.json) and one invalid payload
// (payloads/<kind>.invalid.json) that omits a schema-required field, so a
// consumer can prove both the accept and the fail-closed path of payload
// conformance.
//
//go:embed payloads/*.json
var payloadFS embed.FS

const (
	schemaDir      = "schema"
	payloadDir     = "payloads"
	schemaSuffix   = ".v1.schema.json"
	validSuffix    = ".valid.json"
	invalidSuffix  = ".invalid.json"
	schemaMajorTag = "v1"
)

// Kinds returns the sorted list of fact-kind wire strings the pack carries a
// schema for (for example "aws_resource"). The list is derived from the
// embedded schema files so it can never drift from what the pack actually
// ships.
func Kinds() []string {
	entries, err := schemaFS.ReadDir(schemaDir)
	if err != nil {
		// A read error here means the embed directive lost its files, a build
		// error, not a runtime condition, so a panic surfaces it immediately.
		panic(fmt.Sprintf("fixturepack: read embedded schema dir: %v", err))
	}
	kinds := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, schemaSuffix) {
			continue
		}
		kinds = append(kinds, strings.TrimSuffix(name, schemaSuffix))
	}
	sort.Strings(kinds)
	return kinds
}

// SchemaFor returns the raw JSON Schema bytes for one fact kind. The ok result
// is false when the pack ships no schema for the kind. Callers feed the bytes
// into conformance.Request.PayloadSchemas keyed by the fact kind their own
// collector emits.
func SchemaFor(kind string) (json.RawMessage, bool) {
	raw, err := schemaFS.ReadFile(schemaPath(kind))
	if err != nil {
		return nil, false
	}
	return json.RawMessage(raw), true
}

// Schemas returns every shipped schema keyed by fact kind. It is the accessor
// the in-tree host uses to populate conformance.Request.PayloadSchemas for the
// core kinds, and the drift-lock and construct-coverage tests iterate.
func Schemas() map[string]json.RawMessage {
	kinds := Kinds()
	schemas := make(map[string]json.RawMessage, len(kinds))
	for _, kind := range kinds {
		raw, ok := SchemaFor(kind)
		if !ok {
			continue
		}
		schemas[kind] = raw
	}
	return schemas
}

// ValidPayload returns the curated schema-valid example payload for one fact
// kind, decoded into the map[string]any shape a fact envelope carries. The ok
// result is false when the pack ships no valid payload for the kind.
func ValidPayload(kind string) (map[string]any, bool) {
	return readPayload(payloadPath(kind, validSuffix))
}

// InvalidPayload returns the curated schema-invalid example payload for one
// fact kind — a payload missing a schema-required field — used to prove the
// fail-closed path. The ok result is false when the pack ships no invalid
// payload for the kind.
func InvalidPayload(kind string) (map[string]any, bool) {
	return readPayload(payloadPath(kind, invalidSuffix))
}

// readPayload decodes one embedded payload file into a map, returning ok=false
// when the file is absent. A present-but-malformed file panics because that is
// a build-time authoring error in the pack, not a runtime input condition.
func readPayload(path string) (map[string]any, bool) {
	raw, err := payloadFS.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		panic(fmt.Sprintf("fixturepack: decode embedded payload %q: %v", path, err))
	}
	return payload, true
}

func schemaPath(kind string) string {
	return schemaDir + "/" + kind + schemaSuffix
}

func payloadPath(kind, suffix string) string {
	return payloadDir + "/" + kind + suffix
}

// fsFiles lists the base names under one embedded directory; it exists so the
// parent module's drift-lock test can enumerate the embedded schema set without
// reaching into embed internals.
func fsFiles(dir fs.FS, root string) ([]string, error) {
	entries, err := fs.ReadDir(dir, root)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// SchemaFiles returns the sorted base names of every embedded schema file. It
// backs the drift-lock test in the parent module.
func SchemaFiles() ([]string, error) {
	return fsFiles(schemaFS, schemaDir)
}

// RawSchemaFile returns the raw bytes of one embedded schema file by base name,
// for the drift-lock test's byte comparison against the canonical artifact.
func RawSchemaFile(name string) ([]byte, error) {
	return schemaFS.ReadFile(schemaDir + "/" + name)
}
