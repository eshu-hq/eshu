// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package datazone maps Amazon DataZone governance control-plane metadata into
// AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for DataZone domains,
// projects, environments, and data sources, plus relationships for
// domain-to-KMS-key, domain-to-IAM-role (execution and service roles),
// child-in-domain (project, environment, data source), and
// data-source-to-backing-store (AWS Glue Data Catalog database, provisioned
// Amazon Redshift cluster) evidence. Business glossaries, glossary terms,
// catalog asset content, subscription data, relational filter expressions,
// access credentials, and every mutation API stay outside this package
// contract: the scanner is metadata-only.
package datazone
