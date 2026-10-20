// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package catalog holds the AWS service-category constant families for the
// #6696 collector consolidation: Service*, ResourceType*, and Relationship*
// string constants grouped by service category, consolidated verbatim out of
// the flat constants_<service>.go files in the awscloud root.
//
// The package is a leaf on purpose. The awscloud root uses the Service*
// constants itself (cloudtrail_types.go, apicall.go, redaction.go, types.go),
// and 1,200+ files under awscloud/service import the awscloud root, so
// placing a category's constants beside any service while the root still
// declares them would make the root import the service while the service
// imports the root. Go refuses that cycle. This package imports neither side,
// so both sides can import it without a cycle.
//
// Values here are the collector's wire contract: service-kind strings,
// fact-schema resource-type tokens, and reducer relationship types. A move
// must never change a value. The per-category tests pin every consolidated
// value byte-identical to its pre-consolidation awscloud value.
package catalog
