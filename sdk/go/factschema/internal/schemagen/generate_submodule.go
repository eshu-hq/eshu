// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	submodulev1 "github.com/eshu-hq/eshu/sdk/go/factschema/submodule/v1"
)

// SubmodulePinSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "submodule.pin" payload.
const SubmodulePinSchemaID = schemaBaseID + "submodule/v1/pin.schema.json"

// SubmodulePinSchema returns the JSON Schema bytes for submodulev1.Pin.
func SubmodulePinSchema() ([]byte, error) {
	return reflectSchema(SubmodulePinSchemaID, "Eshu submodule.pin Payload (schema version 1)", &submodulev1.Pin{})
}
