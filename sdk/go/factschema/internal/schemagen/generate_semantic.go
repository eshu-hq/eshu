// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	semanticv1 "github.com/eshu-hq/eshu/sdk/go/factschema/semantic/v1"
)

// SemanticDocumentationObservationSchemaID is the checked-in JSON Schema $id
// for the schema-version-1 "semantic.documentation_observation" payload.
const SemanticDocumentationObservationSchemaID = schemaBaseID + "semantic/v1/documentation_observation.schema.json"

// SemanticDocumentationObservationSchema returns the JSON Schema bytes for
// semanticv1.DocumentationObservation.
func SemanticDocumentationObservationSchema() ([]byte, error) {
	return reflectSchema(SemanticDocumentationObservationSchemaID, "Eshu semantic.documentation_observation Payload (schema version 1)", &semanticv1.DocumentationObservation{})
}

// SemanticCodeHintSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "semantic.code_hint" payload.
const SemanticCodeHintSchemaID = schemaBaseID + "semantic/v1/code_hint.schema.json"

// SemanticCodeHintSchema returns the JSON Schema bytes for semanticv1.CodeHint.
func SemanticCodeHintSchema() ([]byte, error) {
	return reflectSchema(SemanticCodeHintSchemaID, "Eshu semantic.code_hint Payload (schema version 1)", &semanticv1.CodeHint{})
}
