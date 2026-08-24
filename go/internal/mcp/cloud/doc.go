// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudtools defines the MCP registrations for cloud inventory and
// runtime-drift reads.
//
// InventoryTools and RuntimeDriftTools return fresh definitions in their
// canonical local order. The parent mcp package owns global registration order,
// route resolution, dispatch, authorization, response envelopes, and telemetry.
package cloudtools
