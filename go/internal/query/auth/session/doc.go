// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package session holds the browser-session family behind the impact
// handler surface (Issue #6818, move 4a): browser-session wire types,
// cookie issuance and clearing, idle/absolute timeout resolution, and
// the browser-session auth-context normalizers. The session types name
// auth.AuthContext and auth.AuthMode; auth does not import
// session. Callers outside internal/query spell session-qualified names;
// root package query keeps compatibility aliases and forwarders.
package session
