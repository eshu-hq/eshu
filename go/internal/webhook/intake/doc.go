// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package intake owns core's authenticated, versioned webhook intake
// API (#9377). The extracted webhook listener verifies provider signatures,
// normalizes payloads, and submits normalized triggers here over HTTP;
// core retains durable trigger storage and lifecycle through the store
// interfaces in this package. The listener never touches core tables on
// this path.
//
// Wire compatibility: the path carries the major version (/api/v1/...)
// and every request body carries api_version. Absent api_version means the
// oldest v1 client; any other value fails closed with
// unsupported_api_version. Unknown JSON fields are ignored so a newer
// client still submits to this server.
package intake
