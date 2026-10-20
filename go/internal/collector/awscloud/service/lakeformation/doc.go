// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package lakeformation maps AWS Lake Formation data-lake settings, registered
// data locations, and principal/resource permission grants into AWS cloud
// collector facts.
//
// Lake Formation governs the Glue Data Catalog, so the scanner reuses the Glue
// database and table resource_id shapes (bare database name, `database/table`)
// as its permission edge targets. It emits a data-lake settings resource
// (administrator principal identifiers only), one registered-resource resource
// per location with S3-bucket and IAM-role relationships, and one permission
// resource per grant with Glue database/table relationships and an IAM-role
// principal relationship.
//
// The scanner is metadata-only with a data-sensitivity contract: it emits grant
// identities, principal identifiers, and resource ARNs only. It never persists a
// permission policy body, a permission condition (LF-Tag) expression, an LF-Tag
// value, or principal credentials, and it never grants, revokes, registers, or
// deregisters anything. Permission privilege names (SELECT, ALTER, ALL, ...) are
// a closed AWS enum vocabulary recorded as grant identity, not a policy body.
package lakeformation
