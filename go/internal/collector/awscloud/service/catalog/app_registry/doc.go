// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicecatalogappregistry maps AWS Service Catalog AppRegistry
// application and attribute-group metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for AppRegistry applications
// and attribute groups plus relationships for application-to-attribute-group
// membership and application-to-CloudFormation-stack associations. Attribute
// group content bodies (the application-metadata JSON document), associated
// resource tag values, and any mutation API stay outside this package
// contract: the scanner is metadata-only.
package servicecatalogappregistry
