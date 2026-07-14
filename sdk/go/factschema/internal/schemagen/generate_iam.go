// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// AWSIAMPermissionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_iam_permission" payload.
const AWSIAMPermissionSchemaID = schemaBaseID + "iam/v1/permission.schema.json"

// AWSIAMPermissionSchema returns the JSON Schema bytes for iamv1.Permission.
func AWSIAMPermissionSchema() ([]byte, error) {
	return reflectSchema(AWSIAMPermissionSchemaID, "Eshu aws_iam_permission Payload (schema version 1)", &iamv1.Permission{})
}

// AWSResourcePolicyPermissionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_resource_policy_permission" payload.
const AWSResourcePolicyPermissionSchemaID = schemaBaseID + "iam/v1/resource_policy_permission.schema.json"

// AWSResourcePolicyPermissionSchema returns the JSON Schema bytes for
// iamv1.ResourcePolicyPermission.
func AWSResourcePolicyPermissionSchema() ([]byte, error) {
	return reflectSchema(AWSResourcePolicyPermissionSchemaID, "Eshu aws_resource_policy_permission Payload (schema version 1)", &iamv1.ResourcePolicyPermission{})
}

// AWSIAMPrincipalSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "aws_iam_principal" payload.
const AWSIAMPrincipalSchemaID = schemaBaseID + "iam/v1/principal.schema.json"

// AWSIAMPrincipalSchema returns the JSON Schema bytes for iamv1.Principal.
func AWSIAMPrincipalSchema() ([]byte, error) {
	return reflectSchema(AWSIAMPrincipalSchemaID, "Eshu aws_iam_principal Payload (schema version 1)", &iamv1.Principal{})
}
