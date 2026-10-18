// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	refOverlayV1 "github.com/eshu-hq/eshu/sdk/go/factschema/ref_overlay/v1"
)

// RefOverlayBranchObservationSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "ref_overlay.branch_observation" payload.
const RefOverlayBranchObservationSchemaID = schemaBaseID + "ref_overlay/v1/branch_observation.schema.json"

// RefOverlayBranchObservationSchema returns the JSON Schema bytes for
// refOverlayV1.BranchObservation.
func RefOverlayBranchObservationSchema() ([]byte, error) {
	return reflectSchema(RefOverlayBranchObservationSchemaID, "Eshu ref_overlay.branch_observation Payload (schema version 1)", &refOverlayV1.BranchObservation{})
}

// ConfigKeyDivergenceSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "config_key_divergence" payload.
const ConfigKeyDivergenceSchemaID = schemaBaseID + "ref_overlay/v1/config_key_divergence.schema.json"

// ConfigKeyDivergenceSchema returns the JSON Schema bytes for
// refOverlayV1.ConfigKeyDivergence.
func ConfigKeyDivergenceSchema() ([]byte, error) {
	return reflectSchema(ConfigKeyDivergenceSchemaID, "Eshu config_key_divergence Payload (schema version 1)", &refOverlayV1.ConfigKeyDivergence{})
}
