// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// CodegraphFileSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "file" payload.
const CodegraphFileSchemaID = schemaBaseID + "codegraph/v1/file.schema.json"

// CodegraphFileSchema returns the JSON Schema bytes for codegraphv1.File.
func CodegraphFileSchema() ([]byte, error) {
	return reflectSchema(CodegraphFileSchemaID, "Eshu file Payload (schema version 1)", &codegraphv1.File{})
}

// CodegraphRepositorySchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "repository" payload.
const CodegraphRepositorySchemaID = schemaBaseID + "codegraph/v1/repository.schema.json"

// CodegraphRepositorySchema returns the JSON Schema bytes for
// codegraphv1.Repository.
func CodegraphRepositorySchema() ([]byte, error) {
	return reflectSchema(CodegraphRepositorySchemaID, "Eshu repository Payload (schema version 1)", &codegraphv1.Repository{})
}
