// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	securityalertv1 "github.com/eshu-hq/eshu/sdk/go/factschema/securityalert/v1"
)

// SecurityAlertRepositoryAlertSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "security_alert.repository_alert" payload.
const SecurityAlertRepositoryAlertSchemaID = schemaBaseID + "securityalert/v1/repository_alert.schema.json"

// SecurityAlertRepositoryAlertSchema returns the JSON Schema bytes for
// securityalertv1.RepositoryAlert.
func SecurityAlertRepositoryAlertSchema() ([]byte, error) {
	return reflectSchema(SecurityAlertRepositoryAlertSchemaID, "Eshu security_alert.repository_alert Payload (schema version 1)", &securityalertv1.RepositoryAlert{})
}
