// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 API Gateway v2 control-plane
// responses into scanner-owned metadata records.
//
// The adapter pages the API Gateway v2 read-only operations (GetApis,
// GetStages, GetRoutes, GetIntegrations, GetAuthorizers, GetVpcLinks,
// GetDomainNames, GetApiMappings), maps SDK response shapes into the parent
// package's safe metadata model, and records shared AWS collector API-call
// telemetry around every AWS operation. It does not call ExportApi, any
// integration/route response reader, any model/template reader, or any mutation
// API, and it never maps request/response mapping templates, request models,
// authorizer invocation URIs, or credential ARNs. Callers handle AWS
// authorization, throttling, and partial service failures as normal scanner
// errors.
package awssdk
