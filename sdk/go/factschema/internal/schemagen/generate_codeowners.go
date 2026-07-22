// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	codeownersv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codeowners/v1"
)

// CodeownersOwnershipSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "codeowners.ownership" payload.
const CodeownersOwnershipSchemaID = schemaBaseID + "codeowners/v1/ownership.schema.json"

// CodeownersOwnershipSchema returns the JSON Schema bytes for
// codeownersv1.Ownership.
func CodeownersOwnershipSchema() ([]byte, error) {
	return reflectSchema(CodeownersOwnershipSchemaID, "Eshu codeowners.ownership Payload (schema version 1)", &codeownersv1.Ownership{})
}
