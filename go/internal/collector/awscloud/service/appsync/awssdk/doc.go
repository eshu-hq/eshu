// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 AppSync control-plane responses into
// scanner-owned metadata records.
//
// The adapter pages AppSync read-only list operations
// (ListGraphqlApis, ListDataSources, ListTypes, ListResolvers, ListFunctions,
// ListApiKeys) plus GetSchemaCreationStatus, maps SDK response shapes into the
// parent package's safe metadata model, and records shared AWS collector
// API-call telemetry around every AWS operation. It never calls
// EvaluateMappingTemplate, EvaluateCode, GetIntrospectionSchema,
// StartSchemaCreation, GetDataSourceIntrospection, or any mutation API, and it
// never maps the schema SDL body, resolver or function mapping templates,
// function code bodies, or API key values. Callers must handle AWS
// authorization, throttling, and partial service failures as normal scanner
// errors.
package awssdk
