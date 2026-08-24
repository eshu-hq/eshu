// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package doctools defines the MCP registrations for documentation reads.
//
// Tools and FindingAggregateTools return fresh, ordered definitions for the six
// documentation tools. The parent mcp package keeps global registration order,
// route resolution, dispatch, authorization, and telemetry.
// The declared name doctools intentionally distinguishes registration data
// from the documentation domain while the import path remains documentation.
package doctools
