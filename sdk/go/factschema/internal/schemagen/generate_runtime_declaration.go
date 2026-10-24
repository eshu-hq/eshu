// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	runtimedeclarationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1"
)

// RuntimeDeclarationVersionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "runtime_declaration.version" payload.
const RuntimeDeclarationVersionSchemaID = schemaBaseID + "runtimedeclaration/v1/version.schema.json"

// RuntimeDeclarationVersionSchema returns the JSON Schema bytes for
// runtimedeclarationv1.Version. The schema carries no spec_kind enum on
// purpose: SpecKind is a closed set for producers with a tolerant reader, and
// an enum would reject a member added later.
func RuntimeDeclarationVersionSchema() ([]byte, error) {
	return reflectSchema(RuntimeDeclarationVersionSchemaID, "Eshu runtime_declaration.version Payload (schema version 1)", &runtimedeclarationv1.Version{})
}
