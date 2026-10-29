// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 API Gateway control-plane responses
// into scanner-owned metadata records.
//
// The adapter pages REST API Gateway and API Gateway v2 read-only operations,
// maps SDK response shapes into the parent package's safe metadata model, and
// records shared AWS collector API-call telemetry around every AWS operation.
// It does not call API execution, export, API key, authorizer secret, or
// mutation APIs. Dial timeouts and retry-exhausted transport errors degrade to
// exactly one network-timeout warning with the items collected so far (empty
// when the timeout hits the first page) instead of failing the scan;
// callers must still handle AWS authorization, throttling, and other partial
// service failures as normal scanner errors.
package awssdk
